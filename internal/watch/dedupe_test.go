package watch

import (
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	rowstore "weave/internal/store/row"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stamped is the row a loop writes a stamp on, which the renderer draws no column of.
func stamped(id string, at time.Time) rowstore.Row {
	row := agentRow(id)
	row.Agent = agentstore.Agent{
		ID:        row.ID,
		Name:      row.Name,
		UpdatedAt: at,
	}

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
	broker.Signal(watchv1.Resource_RESOURCE_AGENT, "a")

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
	broker.Signal(watchv1.Resource_RESOURCE_AGENT, "a")

	changes := drain(subscription)
	require.Len(t, changes, 1)
	assert.Equal(t, "renamed", changes[0].Row.GetAgent().GetName())

	broker.Signal(watchv1.Resource_RESOURCE_AGENT, "a")
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

	// The first Signal holds the loop while the other ten pile up behind it.
	broker.Signal(watchv1.Resource_RESOURCE_AGENT, "a")
	require.Eventually(t, func() bool { return len(stores.agents.reads()) == 1 }, time.Second, time.Millisecond)

	renamed := agentRow("a")
	renamed.Name = "renamed"
	stores.agents.put(renamed)
	for range 10 {
		broker.Signal(watchv1.Resource_RESOURCE_AGENT, "a")
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

	broker.Signal(watchv1.Resource_RESOURCE_AGENT, "never")
	never := drain(subscription)
	require.Len(t, never, 1, "a key the watcher never held is still sent as gone")
	assert.Nil(t, never[0].Row)

	stores.agents.put()
	broker.Signal(watchv1.Resource_RESOURCE_AGENT, "a")

	changes := drain(subscription)
	require.Len(t, changes, 1)
	assert.Nil(t, changes[0].Row)

	held := subscription.sent[changes[0].Resource]
	assert.NotContains(t, held, "a", "a key that has left the listing is not remembered")

	// The row comes back, and is sent although it is the row the watcher last saw.
	stores.agents.put(agentRow("a"))
	broker.Signal(watchv1.Resource_RESOURCE_AGENT, "a")
	assert.Len(t, drain(subscription), 1)
}
