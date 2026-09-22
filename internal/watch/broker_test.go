package watch

import (
	"errors"
	"fmt"
	"testing"
	"time"

	rowstore "weave/internal/store/row"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settle is long enough for the loop to have rendered whatever was touched.
const settle = 20 * time.Millisecond

// newTestBroker builds a broker rendering over the stores, with no listener behind it and
// watchers let in as if one were up.
func newTestBroker(t *testing.T, stores *fakeStores) *Broker {
	t.Helper()

	broker := NewBroker(NewBrokerParams{Rows: newRows(stores)})
	broker.up()
	t.Cleanup(broker.Close)

	return broker
}

func receive(t *testing.T, subscription *Subscription) Change {
	t.Helper()

	select {
	case change, open := <-subscription.Changes():
		require.True(t, open, "the subscription closed")

		return change
	case <-time.After(2 * time.Second):
		t.Fatal("no change arrived")

		return Change{}
	}
}

// TestTouchesPileUpBehindOneRead pins that touches made while a render is reading are one
// read of the deduplicated ids once it is done.
func TestTouchesPileUpBehindOneRead(t *testing.T) {
	stores := newStores()
	stores.agents.held = make(chan struct{})
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("held"), agentRow("a"), agentRow("b"))
	broker.touch("agents", "held")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	broker.touch("agents", "b")
	broker.touch("agents", "a")
	broker.touch("agents", "a")
	broker.touch("workflows", "w")
	broker.touch("schedules", "s")
	close(stores.agents.held)

	require.Equal(t, "held", receive(t, subscription).Key)
	changes := []Change{receive(t, subscription), receive(t, subscription), receive(t, subscription), receive(t, subscription)}

	assert.Equal(t, [][]string{{"held"}, {"a", "b"}}, stores.agents.reads(), "the touches behind the held read are one read")
	assert.Equal(t, []string{"a", "b", "w", "s"}, []string{changes[0].Key, changes[1].Key, changes[2].Key, changes[3].Key})
	assert.NotNil(t, changes[0].Row)
	assert.Nil(t, changes[2].Row, "a key the store no longer holds is a tombstone")
}

// TestNothingIsReadWithoutAWatcher pins that the broker costs nothing while nobody watches.
func TestNothingIsReadWithoutAWatcher(t *testing.T) {
	stores := newStores()
	broker := newTestBroker(t, stores)

	broker.touch("agents", "a")
	time.Sleep(settle)

	assert.Empty(t, stores.agents.reads())
}

// TestSubscriptionOpensWithTheSnapshot pins that a watcher gets the rows as they stood when it
// subscribed, and every change after.
func TestSubscriptionOpensWithTheSnapshot(t *testing.T) {
	stores := newStores()
	broker := newTestBroker(t, stores)

	first, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Empty(t, first.Snapshot())

	stores.agents.put(agentRow("a"))

	broker.touch("agents", "a")
	require.Equal(t, "a", receive(t, first).Key)

	stores.workflows.put(workflowstore.Workflow{ID: "w"})

	second, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Len(t, second.Snapshot(), 2)

	renamed := agentRow("a")
	renamed.Name = "renamed"
	stores.agents.put(renamed)
	broker.touch("agents", "a")
	assert.Equal(t, "a", receive(t, first).Key)
	assert.Equal(t, "a", receive(t, second).Key)
}

// TestATouchDuringTheSnapshotOnlyReachesWatchersWhoseSnapshotPredatesIt pins that each watcher
// deduplicates against its own snapshot.
func TestATouchDuringTheSnapshotOnlyReachesWatchersWhoseSnapshotPredatesIt(t *testing.T) {
	stores := newStores()
	stores.agents.held = make(chan struct{})
	broker := newTestBroker(t, stores)

	first, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("a"), agentRow("b"))

	// The render is held part way, and a second watcher subscribes behind it.
	broker.touch("agents", "a")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	second, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Len(t, second.Snapshot(), 2)

	broker.touch("agents", "b")
	close(stores.agents.held)

	assert.Equal(t, "a", receive(t, first).Key)
	assert.Equal(t, "b", receive(t, first).Key)
	assert.Empty(t, drain(second), "the second watcher already received both rows in its snapshot")
}

// TestARenderReadBeforeASnapshotSkipsItsWatcher pins that a read taken before a watcher
// subscribed never reaches it.
func TestARenderReadBeforeASnapshotSkipsItsWatcher(t *testing.T) {
	stores := newStores()
	stores.agents.held = make(chan struct{})
	broker := newTestBroker(t, stores)

	first, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("a"))
	broker.touch("agents", "a")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	renamed := agentRow("a")
	renamed.Name = "renamed"
	stores.agents.put(renamed)

	second, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Len(t, second.Snapshot(), 1)
	require.Equal(t, "renamed", second.Snapshot()[0].GetAgent().GetName())

	close(stores.agents.held)

	assert.Equal(t, "agent-a", receive(t, first).Row.GetAgent().GetName())
	assert.Empty(t, drain(second), "the stale read predates the second watcher's snapshot")
}

// TestASlowWatcherIsDropped pins that a watcher that stops reading is closed rather than
// holding the render loop.
func TestASlowWatcherIsDropped(t *testing.T) {
	stores := newStores()
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	// Each round renames every agent, so every round is a change, and nobody reads them.
	ids := make([]string, 26)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	for round := range subscriberBuffer/len(ids) + 2 {
		rows := make([]rowstore.Row, len(ids))
		for i, id := range ids {
			rows[i] = agentRow(id)
			rows[i].Name = fmt.Sprintf("agent-%s-%d", id, round)
		}
		stores.agents.put(rows...)

		for _, id := range ids {
			broker.touch("agents", id)
		}
		time.Sleep(settle)
	}

	require.Eventually(t, func() bool {
		for {
			select {
			case _, open := <-subscription.Changes():
				if !open {
					return true
				}
			default:
				return false
			}
		}
	}, 2*time.Second, settle, "the stream closes once the watcher has fallen too far behind")
}

// TestCloseEndsEverySubscription pins that closing the broker closes every stream and refuses
// new watchers, and that closing twice and touching afterwards are safe.
func TestCloseEndsEverySubscription(t *testing.T) {
	broker := NewBroker(NewBrokerParams{Rows: newRows(newStores())})
	broker.up()

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	broker.Close()
	broker.Close()

	_, open := <-subscription.Changes()
	require.False(t, open)

	_, err = broker.Subscribe(t.Context())
	require.ErrorIs(t, err, ErrClosed)

	broker.touch("agents", "a")
	subscription.Close()
}

// TestAFailedReadIsRetriedWithTheNextOne pins that a read that fails keeps its touches for the
// next render, which reads them again beside whatever was touched since.
func TestAFailedReadIsRetriedWithTheNextOne(t *testing.T) {
	stores := newStores()
	stores.agents.err = errors.New("boom")
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("a"), agentRow("b"))
	broker.touch("agents", "a")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	stores.agents.mu.Lock()
	stores.agents.err = nil
	stores.agents.mu.Unlock()

	broker.touch("agents", "b")
	assert.Equal(t, "a", receive(t, subscription).Key, "the touch the failed read lost comes back")
	assert.Equal(t, "b", receive(t, subscription).Key)
	assert.Equal(t, []string{"a", "b"}, stores.agents.reads()[1], "one read serves both")
}

// TestClosingASubscriptionEndsItsStream pins that a watcher leaving closes its stream and, as
// the last one, stops the reads.
func TestClosingASubscriptionEndsItsStream(t *testing.T) {
	stores := newStores()
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	subscription.Close()
	subscription.Close()

	_, open := <-subscription.Changes()
	require.False(t, open)

	broker.touch("agents", "a")
	time.Sleep(settle)
	assert.Empty(t, stores.agents.reads(), "nothing is read once the last watcher has gone")
}

// TestABrokerNeverStartedStillCloses pins that a broker built and never started takes no
// watcher, closes cleanly and takes none after.
func TestABrokerNeverStartedStillCloses(t *testing.T) {
	broker := NewBroker(NewBrokerParams{})

	_, err := broker.Subscribe(t.Context())
	require.ErrorIs(t, err, ErrNotListening)

	broker.touch("agents", "a")
	broker.Close()

	_, err = broker.Subscribe(t.Context())
	require.ErrorIs(t, err, ErrClosed)
}

// TestHearReadsTheTableAndTheKey pins the payload a trigger raises being split at its one
// space, with a table no listing is keyed by and a payload with no key both ignored.
func TestHearReadsTheTableAndTheKey(t *testing.T) {
	stores := newStores()
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	broker.hear("river_job 7")
	broker.hear("agents")
	broker.hear("agents a b")

	assert.Equal(t, "a b", receive(t, subscription).Key, "the key runs to the end of the payload")
	assert.Equal(t, [][]string{{"a b"}}, stores.agents.reads())
}
