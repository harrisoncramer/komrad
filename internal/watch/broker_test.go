package watch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settle is long enough for the loop to have rendered whatever was touched.
const settle = 20 * time.Millisecond

type fakeReader struct {
	mu       sync.Mutex
	reads    []Touched
	snapshot []*watchv1.Row
	readErr  error
	// held blocks Read while it is set, so a test can pile touches up behind one render.
	held chan struct{}
}

func (f *fakeReader) Read(_ context.Context, touched Touched) ([]Change, error) {
	f.mu.Lock()
	held, readErr := f.held, f.readErr
	f.reads = append(f.reads, touched)
	f.mu.Unlock()

	if held != nil {
		<-held
	}
	if readErr != nil {
		return nil, readErr
	}

	changes := xslice.Map(touched[watchv1.Resource_RESOURCE_AGENT], func(id string) Change {
		return Change{Resource: watchv1.Resource_RESOURCE_AGENT, Key: id, Row: &watchv1.Row{Row: &watchv1.Row_Agent{Agent: &agentv1.Agent{Id: id}}}}
	})
	changes = append(changes, xslice.Map(touched[watchv1.Resource_RESOURCE_WORKFLOW], func(id string) Change { return Change{Resource: watchv1.Resource_RESOURCE_WORKFLOW, Key: id} })...)
	changes = append(changes, xslice.Map(touched[watchv1.Resource_RESOURCE_SCHEDULE], func(id string) Change { return Change{Resource: watchv1.Resource_RESOURCE_SCHEDULE, Key: id} })...)

	return changes, nil
}

func (f *fakeReader) Snapshot(context.Context) ([]*watchv1.Row, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.snapshot, nil
}

func (f *fakeReader) getReads() []Touched {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]Touched(nil), f.reads...)
}

// newTestBroker builds a broker rendering through the reader, with no listener behind it and
// watchers let in as if one were up.
func newTestBroker(t *testing.T, reader *fakeReader) *Broker {
	t.Helper()

	broker := NewBroker(NewBrokerParams{})
	broker.serve(reader)
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
	reader := &fakeReader{held: make(chan struct{})}
	broker := newTestBroker(t, reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	broker.touch("agents", "held")
	require.Eventually(t, func() bool { return len(reader.getReads()) == 1 }, time.Second, time.Millisecond)

	broker.touch("agents", "b", "a")
	broker.touch("agents", "a")
	broker.touch("workflows", "w")
	broker.touch("schedules", "s")
	close(reader.held)

	require.Equal(t, "held", receive(t, subscription).Key)
	changes := []Change{receive(t, subscription), receive(t, subscription), receive(t, subscription), receive(t, subscription)}

	require.Equal(t, Touched{watchv1.Resource_RESOURCE_AGENT: []string{"a", "b"}, watchv1.Resource_RESOURCE_WORKFLOW: []string{"w"}, watchv1.Resource_RESOURCE_SCHEDULE: []string{"s"}}, reader.getReads()[1])
	require.Equal(t, []string{"a", "b", "w", "s"}, []string{changes[0].Key, changes[1].Key, changes[2].Key, changes[3].Key})
	require.NotNil(t, changes[0].Row)
	require.Nil(t, changes[2].Row, "a row the reader did not render is a tombstone")
}

// TestNothingIsReadWithoutAWatcher pins that the broker costs nothing while nobody watches.
func TestNothingIsReadWithoutAWatcher(t *testing.T) {
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	broker.touch("agents", "a")
	time.Sleep(settle)

	require.Empty(t, reader.getReads())
}

// TestSubscriptionOpensWithTheSnapshot pins that a watcher gets the rows as they stood when it
// subscribed, and every change after.
func TestSubscriptionOpensWithTheSnapshot(t *testing.T) {
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	first, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Empty(t, first.Snapshot())

	broker.touch("agents", "a")
	require.Equal(t, "a", receive(t, first).Key)

	reader.mu.Lock()
	reader.snapshot = []*watchv1.Row{
		{Row: &watchv1.Row_Agent{Agent: &agentv1.Agent{Id: "a"}}},
		{Row: &watchv1.Row_Workflow{Workflow: &workflowv1.Workflow{Id: "w"}}},
	}
	reader.mu.Unlock()

	second, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Len(t, second.Snapshot(), 2)

	broker.touch("agents", "a")
	require.Equal(t, "a", receive(t, first).Key)
	require.Equal(t, "a", receive(t, second).Key)
}

// TestATouchDuringTheSnapshotLandsAfterIt pins the ordering a snapshot relies on: a touch
// pending while a watcher subscribes renders after its snapshot and reaches it.
func TestATouchDuringTheSnapshotLandsAfterIt(t *testing.T) {
	reader := &fakeReader{held: make(chan struct{})}
	broker := newTestBroker(t, reader)

	first, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	// The render is held part way, and a second watcher subscribes behind it.
	broker.touch("agents", "a")
	require.Eventually(t, func() bool { return len(reader.getReads()) == 1 }, time.Second, time.Millisecond)

	second, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	broker.touch("agents", "b")
	close(reader.held)

	require.Equal(t, "a", receive(t, first).Key)
	require.Equal(t, "a", receive(t, second).Key, "a render in flight as a watcher subscribes reaches it")
	require.Equal(t, "b", receive(t, first).Key)
	require.Equal(t, "b", receive(t, second).Key)
}

// TestASlowWatcherIsDropped pins that a watcher that stops reading is closed rather than
// holding the render loop.
func TestASlowWatcherIsDropped(t *testing.T) {
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	// Each round renders one change per letter, and nobody reads them.
	ids := make([]string, 26)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	for range subscriberBuffer/len(ids) + 2 {
		broker.touch("agents", ids...)
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
	reader := &fakeReader{}
	broker := NewBroker(NewBrokerParams{})
	broker.serve(reader)
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
	reader := &fakeReader{readErr: errors.New("boom")}
	broker := newTestBroker(t, reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	broker.touch("agents", "a")
	require.Eventually(t, func() bool { return len(reader.getReads()) == 1 }, time.Second, time.Millisecond)

	reader.mu.Lock()
	reader.readErr = nil
	reader.mu.Unlock()

	broker.touch("agents", "b")
	assert.Equal(t, "a", receive(t, subscription).Key, "the touch the failed read lost comes back")
	assert.Equal(t, "b", receive(t, subscription).Key)
	assert.Equal(t, Touched{watchv1.Resource_RESOURCE_AGENT: []string{"a", "b"}}, reader.getReads()[1], "one read serves both")
}

// TestClosingASubscriptionEndsItsStream pins that a watcher leaving closes its stream and, as
// the last one, stops the reads.
func TestClosingASubscriptionEndsItsStream(t *testing.T) {
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	subscription.Close()
	subscription.Close()

	_, open := <-subscription.Changes()
	require.False(t, open)

	broker.touch("agents", "a")
	time.Sleep(settle)
	require.Empty(t, reader.getReads(), "nothing is read once the last watcher has gone")
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
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	broker.hear("river_job 7")
	broker.hear("agents")
	broker.hear("agents a b")

	require.Equal(t, "a b", receive(t, subscription).Key, "the key runs to the end of the payload")
	require.Equal(t, []Touched{{watchv1.Resource_RESOURCE_AGENT: []string{"a b"}}}, reader.getReads())
}
