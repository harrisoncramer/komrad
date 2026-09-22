package watch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"weave/internal/store"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCoalesce = 5 * time.Millisecond

type fakeReader struct {
	mu       sync.Mutex
	reads    []Touched
	snapshot []Event
	readErr  error
	// held blocks Read while it is set, so a test can pile touches up behind one render.
	held chan struct{}
}

func (f *fakeReader) Read(_ context.Context, touched Touched) ([]Event, error) {
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

	events := xslice.Map(touched.Agents, func(id string) Event { return Event{Kind: KindAgent, ID: id, Agent: &agentv1.Agent{Id: id}} })
	events = append(events, xslice.Map(touched.Workflows, func(id string) Event { return Event{Kind: KindWorkflow, ID: id} })...)
	events = append(events, xslice.Map(touched.Schedules, func(id string) Event { return Event{Kind: KindSchedule, ID: id} })...)

	return events, nil
}

func (f *fakeReader) Snapshot(context.Context) ([]Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.snapshot, nil
}

func (f *fakeReader) getReads() []Touched {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]Touched(nil), f.reads...)
}

func newTestBroker(t *testing.T, reader *fakeReader) *Broker {
	t.Helper()

	broker := NewBroker(NewBrokerParams{Coalesce: testCoalesce})
	broker.Start(reader)
	t.Cleanup(broker.Close)

	return broker
}

func receive(t *testing.T, subscription *Subscription) Event {
	t.Helper()

	select {
	case event, open := <-subscription.Events():
		require.True(t, open, "the subscription closed")

		return event
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")

		return Event{}
	}
}

func drainSnapshot(t *testing.T, subscription *Subscription) []Event {
	t.Helper()

	var rows []Event
	for {
		event := receive(t, subscription)
		if event.Synced {
			return rows
		}
		rows = append(rows, event)
	}
}

// TestTouchesCoalesceIntoOneRead pins that touches within the window are one read of the
// deduplicated ids, each event stamped with the next seq.
func TestTouchesCoalesceIntoOneRead(t *testing.T) {
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Empty(t, drainSnapshot(t, subscription))

	broker.Touch(store.TableAgents, "b", "a")
	broker.Touch(store.TableAgents, "a")
	broker.Touch(store.TableWorkflows, "w")
	broker.Touch(store.TableSchedules, "s")

	events := []Event{receive(t, subscription), receive(t, subscription), receive(t, subscription), receive(t, subscription)}

	require.Equal(t, []Touched{{Agents: []string{"a", "b"}, Workflows: []string{"w"}, Schedules: []string{"s"}}}, reader.getReads())
	require.Equal(t, []string{"a", "b", "w", "s"}, []string{events[0].ID, events[1].ID, events[2].ID, events[3].ID})
	for i, event := range events {
		require.Equal(t, uint64(i+1), event.Seq, "the seq counts up one per event")
	}
	require.NotNil(t, events[0].Agent)
	require.True(t, events[2].IsTombstone(), "a row the reader did not render is a tombstone")
}

// TestNothingIsReadWithoutAWatcher pins that the broker costs nothing while nobody watches.
func TestNothingIsReadWithoutAWatcher(t *testing.T) {
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	broker.Touch(store.TableAgents, "a")
	time.Sleep(10 * testCoalesce)

	require.Empty(t, reader.getReads())
}

// TestSnapshotCarriesTheLastSeqAndLiveEventsFollow pins the stamp on a snapshot row and that
// a render after it reaches every watcher with a higher seq.
func TestSnapshotCarriesTheLastSeqAndLiveEventsFollow(t *testing.T) {
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	first, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Empty(t, drainSnapshot(t, first))

	broker.Touch(store.TableAgents, "a")
	require.Equal(t, uint64(1), receive(t, first).Seq)

	reader.mu.Lock()
	reader.snapshot = []Event{
		{Kind: KindAgent, ID: "a", Agent: &agentv1.Agent{Id: "a"}},
		{Kind: KindWorkflow, ID: "w", Workflow: &workflowv1.Workflow{Id: "w"}},
	}
	reader.mu.Unlock()

	second, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	rows := drainSnapshot(t, second)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Equal(t, uint64(1), row.Seq, "a snapshot row carries the seq of the last render")
	}

	broker.Touch(store.TableAgents, "a")
	require.Equal(t, uint64(2), receive(t, first).Seq)
	require.Equal(t, uint64(2), receive(t, second).Seq)
}

// TestATouchDuringTheSnapshotLandsAfterIt pins the ordering a snapshot relies on: a touch
// pending while a watcher subscribes renders after its snapshot and reaches it.
func TestATouchDuringTheSnapshotLandsAfterIt(t *testing.T) {
	reader := &fakeReader{held: make(chan struct{})}
	broker := newTestBroker(t, reader)

	first, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Empty(t, drainSnapshot(t, first))

	// The render is held part way, and a second watcher subscribes behind it.
	broker.Touch(store.TableAgents, "a")
	require.Eventually(t, func() bool { return len(reader.getReads()) == 1 }, time.Second, time.Millisecond)

	subscribed := make(chan *Subscription, 1)
	go func() {
		second, err := broker.Subscribe(t.Context())
		if err != nil {
			panic(err)
		}
		subscribed <- second
	}()

	broker.Touch(store.TableAgents, "b")
	close(reader.held)

	require.Equal(t, "a", receive(t, first).ID)
	second := <-subscribed
	require.Empty(t, drainSnapshot(t, second))

	// The touch that was pending when the second watcher subscribed reaches both.
	require.Equal(t, "b", receive(t, first).ID)
	require.Equal(t, "b", receive(t, second).ID)
}

// TestASlowWatcherIsDropped pins that a watcher that stops reading is closed rather than
// holding the render goroutine.
func TestASlowWatcherIsDropped(t *testing.T) {
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)

	// Each round renders one event per letter, and nobody reads them.
	ids := make([]string, 26)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	for range subscriberBuffer/len(ids) + 2 {
		broker.Touch(store.TableAgents, ids...)
		time.Sleep(2 * testCoalesce)
	}

	require.Eventually(t, func() bool {
		for {
			select {
			case _, open := <-subscription.Events():
				if !open {
					return true
				}
			default:
				return false
			}
		}
	}, 2*time.Second, testCoalesce, "the stream closes once the watcher has fallen too far behind")
}

// TestCloseEndsEverySubscription pins that closing the broker closes every stream and refuses
// new watchers, and that closing twice is safe.
func TestCloseEndsEverySubscription(t *testing.T) {
	reader := &fakeReader{}
	broker := NewBroker(NewBrokerParams{Coalesce: testCoalesce})
	broker.Start(reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Empty(t, drainSnapshot(t, subscription))

	broker.Close()
	broker.Close()

	_, open := <-subscription.Events()
	require.False(t, open)

	_, err = broker.Subscribe(t.Context())
	require.ErrorIs(t, err, ErrClosed)

	broker.Touch(store.TableAgents, "a")
	subscription.Close()
}

// TestAFailedReadIsRetriedWithTheNextOne pins that a read that fails keeps its touches for the
// next render, which reads them again beside whatever was touched since.
func TestAFailedReadIsRetriedWithTheNextOne(t *testing.T) {
	reader := &fakeReader{readErr: errors.New("boom")}
	broker := newTestBroker(t, reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Empty(t, drainSnapshot(t, subscription))

	broker.Touch(store.TableAgents, "a")
	require.Eventually(t, func() bool { return len(reader.getReads()) == 1 }, time.Second, time.Millisecond)

	reader.mu.Lock()
	reader.readErr = nil
	reader.mu.Unlock()

	broker.Touch(store.TableAgents, "b")
	assert.Equal(t, "a", receive(t, subscription).ID, "the touch the failed read lost comes back")
	assert.Equal(t, "b", receive(t, subscription).ID)
	assert.Equal(t, Touched{Agents: []string{"a", "b"}}, reader.getReads()[1], "one read serves both")
}

// TestClosingASubscriptionEndsItsStream pins that a watcher leaving closes its stream and, as
// the last one, stops the reads.
func TestClosingASubscriptionEndsItsStream(t *testing.T) {
	reader := &fakeReader{}
	broker := newTestBroker(t, reader)

	subscription, err := broker.Subscribe(t.Context())
	require.NoError(t, err)
	require.Empty(t, drainSnapshot(t, subscription))

	subscription.Close()
	subscription.Close()

	_, open := <-subscription.Events()
	require.False(t, open)

	broker.Touch(store.TableAgents, "a")
	time.Sleep(4 * testCoalesce)
	require.Empty(t, reader.getReads(), "nothing is read once the last watcher has gone")
}

// TestABrokerNeverStartedStillCloses pins that a broker built and never started closes cleanly.
func TestABrokerNeverStartedStillCloses(t *testing.T) {
	broker := NewBroker(NewBrokerParams{})
	broker.Touch(store.TableAgents, "a")
	broker.Close()

	_, err := broker.Subscribe(t.Context())
	require.ErrorIs(t, err, ErrClosed)
}
