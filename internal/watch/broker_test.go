package watch

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settle is long enough for the loop to have rendered whatever was pending.
const settle = 20 * time.Millisecond

// newTestBroker builds a broker rendering over the stores, with no listener behind it and
// watchers let in as if one were up.
func newTestBroker(t *testing.T, stores *fakeStores) *Broker {
	t.Helper()

	broker := NewBroker(NewBrokerParams{
		Rows:      newRows(stores),
		Resources: testResources,
		PassGap:   time.Microsecond,
	})
	broker.up()
	t.Cleanup(broker.Close)

	return broker
}

// TestPendingKeysPileUpBehindOneRead pins that keys marked pending while a render is reading are one
// read of the deduplicated ids once it is done.
func TestPendingKeysPileUpBehindOneRead(t *testing.T) {
	stores := newStores()
	stores.agents.held = make(chan struct{})
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("held"), agentRow("a"), agentRow("b"))
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "held")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "b")
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	broker.signal(watchv1.Resource_RESOURCE_WORKFLOW, "w")
	close(stores.agents.held)

	require.Equal(t, []string{"held"}, keysInPass(waitForNextPass(t, subscription)))
	pass := waitForNextPass(t, subscription)

	assert.Equal(t, [][]string{{"held"}, {"a", "b"}}, stores.agents.reads(), "the keys pending behind the held read are one read")
	require.Equal(t, []string{"a", "b", "w"}, keysInPass(pass), "everything pending behind the held read is one pass")
	assert.NotNil(t, pass[0].Row)
	assert.Nil(t, pass[2].Row, "a key the watcher never held is still sent as gone")
	assert.Empty(t, drain(subscription))
}

// TestABurstIsOnePassAndKeepsTheWatcher pins that thousands of keys pending behind one read reach
// a watcher as a single pass rather than overflowing its buffer.
func TestABurstIsOnePassAndKeepsTheWatcher(t *testing.T) {
	stores := newStores()
	stores.agents.held = make(chan struct{})
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("held"))
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "held")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	burst := 3 * subscriberBuffer
	for i := range burst {
		broker.signal(watchv1.Resource_RESOURCE_WORKFLOW, fmt.Sprintf("w-%d", i))
	}
	close(stores.agents.held)

	require.Equal(t, []string{"held"}, keysInPass(waitForNextPass(t, subscription)))
	assert.Len(t, waitForNextPass(t, subscription), burst, "the whole burst is one pass")

	renamed := agentRow("held")
	renamed.Name = "renamed"
	stores.agents.put(renamed)
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "held")
	assert.Equal(t, []string{"held"}, keysInPass(waitForNextPass(t, subscription)), "and the watcher is still connected")
}

// TestPassesAreSpacedByTheGap pins the least time the broker leaves between two passes.
func TestPassesAreSpacedByTheGap(t *testing.T) {
	stores := newStores()
	gap := 100 * time.Millisecond
	broker := NewBroker(NewBrokerParams{Rows: newRows(stores), PassGap: gap})
	broker.up()
	t.Cleanup(broker.Close)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("a"), agentRow("b"))
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	waitForNextPass(t, subscription)
	first := time.Now()

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "b")
	waitForNextPass(t, subscription)

	assert.GreaterOrEqual(t, time.Since(first), gap/2, "the second pass waits out the gap")
}

// TestNothingIsReadWithoutAWatcher pins that the broker costs nothing while nobody watches.
func TestNothingIsReadWithoutAWatcher(t *testing.T) {
	stores := newStores()
	broker := newTestBroker(t, stores)

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
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

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	require.Equal(t, []string{"a"}, keysInPass(waitForNextPass(t, first)))

	stores.workflows.put(testRow{ID: "w"})

	second, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Len(t, second.Snapshot(), 2)

	renamed := agentRow("a")
	renamed.Name = "renamed"
	stores.agents.put(renamed)
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	assert.Equal(t, []string{"a"}, keysInPass(waitForNextPass(t, first)))
	assert.Equal(t, []string{"a"}, keysInPass(waitForNextPass(t, second)))
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
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	second, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Len(t, second.Snapshot(), 2)

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "b")
	close(stores.agents.held)

	assert.Equal(t, []string{"a"}, keysInPass(waitForNextPass(t, first)))
	assert.Equal(t, []string{"b"}, keysInPass(waitForNextPass(t, first)))
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
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	renamed := agentRow("a")
	renamed.Name = "renamed"
	stores.agents.put(renamed)

	second, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Len(t, second.Snapshot(), 1)
	require.Equal(t, "renamed", second.Snapshot()[0].GetAgent().GetName())

	close(stores.agents.held)

	assert.Equal(t, "agent-a", waitForNextPass(t, first)[0].Row.GetAgent().GetName())
	assert.Empty(t, drain(second), "the stale read predates the second watcher's snapshot")
}

// TestASlowWatcherIsDropped pins that a watcher that stops reading is closed rather than
// holding the render loop.
func TestASlowWatcherIsDropped(t *testing.T) {
	stores := newStores()
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	// Each round renames the agent and waits for its pass to be queued, so every round is a pass
	// nobody reads.
	for round := range subscriberBuffer + 1 {
		row := agentRow("a")
		row.Name = fmt.Sprintf("agent-a-%d", round)
		stores.agents.put(row)

		broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
		require.Eventually(t, func() bool {
			return len(subscription.changes) == min(round+1, subscriberBuffer)
		}, time.Second, time.Microsecond)
	}

	require.Eventually(t, func() bool {
		broker.mu.Lock()
		defer broker.mu.Unlock()

		return len(broker.subscribers) == 0
	}, 2*time.Second, time.Millisecond, "the broker lets go of the watcher that fell too far behind")

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

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	subscription.Close()
}

// TestAFailedReadIsRetriedWithTheNextOne pins that a read that fails keeps its pending keys for the
// next render, which reads them again beside whatever was marked pending since.
func TestAFailedReadIsRetriedWithTheNextOne(t *testing.T) {
	stores := newStores()
	stores.agents.err = errors.New("boom")
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("a"), agentRow("b"))
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	stores.agents.mu.Lock()
	stores.agents.err = nil
	stores.agents.mu.Unlock()

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "b")
	assert.Equal(t, []string{"a", "b"}, keysInPass(waitForNextPass(t, subscription)), "the key the failed read lost comes back beside the next")
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

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	time.Sleep(settle)
	assert.Empty(t, stores.agents.reads(), "nothing is read once the last watcher has gone")
}

// TestABrokerNeverStartedStillCloses pins that a broker built and never started takes no
// watcher, closes cleanly and takes none after.
func TestABrokerNeverStartedStillCloses(t *testing.T) {
	broker := NewBroker(NewBrokerParams{})

	_, err := broker.Subscribe(t.Context())
	require.ErrorIs(t, err, ErrNotListening)

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
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

	stores.agents.put(agentRow("a b"))
	broker.handleNotification("river_job 7")
	broker.handleNotification("agents")
	broker.handleNotification("agents a b")

	assert.Equal(t, []string{"a b"}, keysInPass(waitForNextPass(t, subscription)), "the key runs to the end of the payload")
	assert.Equal(t, [][]string{{"a b"}}, stores.agents.reads())
}

// TestASignalForAResourceNothingReadsIsDropped pins that a resource with no loader never reaches
// the render, and the pass carries only what is read.
func TestASignalForAResourceNothingReadsIsDropped(t *testing.T) {
	stores := newStores()
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("a"))
	broker.signal(watchv1.Resource_RESOURCE_UNSPECIFIED, "u")
	broker.signal(watchv1.Resource_RESOURCE_REPOSITORY, "https://example.com/repo")
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")

	pass := waitForNextPass(t, subscription)
	assert.Equal(t, []string{"a"}, keysInPass(pass))
	assert.Equal(t, watchv1.Resource_RESOURCE_AGENT, pass[0].Resource)
}

// waitForNextPass waits for the next pass to reach a subscription.
func waitForNextPass(t *testing.T, subscription *Subscription) []Change {
	t.Helper()

	select {
	case pass, open := <-subscription.Changes():
		require.True(t, open, "the subscription closed")

		return pass
	case <-time.After(2 * time.Second):
		t.Fatal("no pass arrived")

		return nil
	}
}

// waitForPassOfOneChange waits for the next pass and requires it to hold a single change.
func waitForPassOfOneChange(t *testing.T, subscription *Subscription) Change {
	t.Helper()

	pass := waitForNextPass(t, subscription)
	require.Len(t, pass, 1, "the pass holds one change")

	return pass[0]
}

// keysInPass is the key each change in a pass names, in order.
func keysInPass(pass []Change) []string {
	return xslice.Map(pass, func(change Change) string { return change.Key })
}

// stamped is the row a loop writes a stamp on, which the renderer draws no column of.
func stamped(id string, at time.Time) testRow {
	row := agentRow(id)
	row.UpdatedAt = at

	return row
}

// drain reports whether anything reached a subscription within the settle.
func drain(subscription *Subscription) []Change {
	var changes []Change
	for {
		select {
		case pass, open := <-subscription.Changes():
			if !open {
				return changes
			}

			changes = append(changes, pass...)
		case <-time.After(settle):
			return changes
		}
	}
}

// TestAStampOnlyUpdateSendsNothing pins the reason the triggers may be dumb: a write that moves
// only a column no reader draws raises its notification, is read back, and goes no further.
func TestAStampOnlyUpdateSendsNothing(t *testing.T) {
	stores := newStores()
	stores.agents.put(stamped("a", time.Unix(1, 0)))
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Len(t, subscription.Snapshot(), 1)

	stores.agents.put(stamped("a", time.Unix(2, 0)))
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")

	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond, "the row is read back")
	assert.Empty(t, drain(subscription), "the rendered row is the one the watcher holds")
}

// TestARealChangeSendsOnce pins that a change a reader draws goes out, and that touching the
// same row again with nothing moved goes nowhere.
func TestARealChangeSendsOnce(t *testing.T) {
	stores := newStores()
	stores.agents.put(agentRow("a"))
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	renamed := agentRow("a")
	renamed.Name = "renamed"
	stores.agents.put(renamed)
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")

	changes := drain(subscription)
	require.Len(t, changes, 1)
	assert.Equal(t, "renamed", changes[0].Row.GetAgent().GetName())

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	assert.Empty(t, drain(subscription), "the same row read back twice is sent once")
}

// TestARowTouchedTenTimesInOnePassSendsOnce pins the pending set: the keys behind one render
// are one read and one change.
func TestARowTouchedTenTimesInOnePassSendsOnce(t *testing.T) {
	stores := newStores()
	stores.agents.held = make(chan struct{})
	stores.agents.put(agentRow("a"))
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	// The first signal holds the loop while the other ten pile up behind it.
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	renamed := agentRow("a")
	renamed.Name = "renamed"
	stores.agents.put(renamed)
	for range 10 {
		broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	}
	close(stores.agents.held)

	changes := drain(subscription)
	require.Len(t, changes, 1, "ten notifications for one row are one change")
	assert.Equal(t, "renamed", changes[0].Row.GetAgent().GetName())
}

// TestATombstoneSendsWhetherOrNotTheRowWasHeld pins that a key outside the watched set goes out
// as gone for every watcher, and that one leaving it is no longer remembered.
func TestATombstoneSendsWhetherOrNotTheRowWasHeld(t *testing.T) {
	stores := newStores()
	stores.agents.put(agentRow("a"))
	broker := newTestBroker(t, stores)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	broker.signal(watchv1.Resource_RESOURCE_AGENT, "never")
	never := drain(subscription)
	require.Len(t, never, 1, "a key the watcher never held is still sent as gone")
	assert.Nil(t, never[0].Row)

	stores.agents.put()
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")

	changes := drain(subscription)
	require.Len(t, changes, 1)
	assert.Nil(t, changes[0].Row)

	held := subscription.sent[changes[0].Resource]
	assert.NotContains(t, held, "a", "a key that has left the listing is not remembered")

	// The row comes back, and is sent although it is the row the watcher last saw.
	stores.agents.put(agentRow("a"))
	broker.signal(watchv1.Resource_RESOURCE_AGENT, "a")
	assert.Len(t, drain(subscription), 1)
}
