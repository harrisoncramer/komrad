package watch

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"weave/internal/fail"
	"weave/internal/logging"
	"weave/internal/store"
	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
)

// subscriberBuffer is how many live events a watcher may fall behind before it is dropped.
const subscriberBuffer = 1024

// readTimeout is how long one read-back or snapshot may take.
const readTimeout = 10 * time.Second

// retryAfter is how long a render that failed to read waits before the next one.
const retryAfter = time.Second

// ErrClosed says the broker has stopped and takes no more watchers.
var ErrClosed = fail.Precondition("the watch broker is closed")

// Kind is which listing a row is on.
type Kind int

const (
	KindAgent Kind = iota + 1
	KindWorkflow
	KindSchedule
)

// kinds is the listing each table a store names belongs to.
var kinds = map[string]Kind{
	store.TableAgents:    KindAgent,
	store.TableWorkflows: KindWorkflow,
	store.TableSchedules: KindSchedule,
}

// Touched is the ids one render reads back, by kind.
type Touched struct {
	Agents    []string
	Workflows []string
	Schedules []string
}

// Event is one row as a watcher should now hold it, or a tombstone for one it should drop.
type Event struct {
	Kind Kind
	ID   string
	// Agent, Workflow and Schedule carry the row, and all three nil is a tombstone.
	Agent    *agentv1.Agent
	Workflow *workflowv1.Workflow
	Schedule *schedulev1.Schedule
}

// IsTombstone reports whether the event says the row is gone from its listing.
func (e Event) IsTombstone() bool {
	return e.Agent == nil && e.Workflow == nil && e.Schedule == nil
}

// Snapshot is every row the three listings hold.
type Snapshot struct {
	Agents    []*agentv1.Agent
	Workflows []*workflowv1.Workflow
	Schedules []*schedulev1.Schedule
}

// Reader reads the rows a render names back, and every row for a snapshot.
type Reader interface {
	// Read renders each touched id as its row, or as a tombstone when its listing no longer
	// holds it.
	Read(ctx context.Context, touched Touched) ([]Event, error)
	// Snapshot renders every row the listings hold.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// NewBrokerParams is what a broker is built over.
type NewBrokerParams struct {
	Logger *slog.Logger
}

// Broker takes touches from every store, reads the rows back and fans them out to every open
// subscription.
type Broker struct {
	logger *slog.Logger
	reader Reader

	// pendingMu guards what has been touched and not yet read.
	pendingMu sync.Mutex
	pending   map[Kind]map[string]struct{}
	// wake has room for one poke, which is all the loop needs to know.
	wake chan struct{}

	// mu guards the subscribers and whether the broker has closed.
	mu          sync.Mutex
	subscribers map[*Subscription]struct{}
	closed      bool
}

// NewBroker builds the broker, which takes touches at once and renders none of them until
// Start names what reads the rows back.
func NewBroker(params NewBrokerParams) *Broker {
	logger := params.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &Broker{
		logger:      logger,
		pending:     map[Kind]map[string]struct{}{},
		wake:        make(chan struct{}, 1),
		subscribers: map[*Subscription]struct{}{},
	}
}

// Start begins rendering through the reader, which Close ends.
func (b *Broker) Start(reader Reader) {
	b.reader = reader

	go func() {
		for range b.wake {
			if !b.render() {
				time.Sleep(retryAfter)
			}
		}
	}()
}

// Touch says the rows of a listing with these ids may have moved.
func (b *Broker) Touch(table string, ids ...string) {
	kind, ok := kinds[table]
	if !ok {
		b.logger.LogAttrs(context.Background(), slog.LevelWarn, "a touch named a table no listing is keyed by", slog.String(logging.WatchTable, table))

		return
	}
	if len(ids) == 0 {
		return
	}

	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	if b.pending == nil {
		return
	}

	touched := b.pending[kind]
	if touched == nil {
		touched = map[string]struct{}{}
		b.pending[kind] = touched
	}
	for _, id := range ids {
		touched[id] = struct{}{}
	}

	// Poke the loop unless it has an unread poke.
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// render reads back everything touched since the last one and sends it to every subscriber,
// reporting false when the read failed and the ids are back in the pending set.
func (b *Broker) render() bool {
	touched := b.take()

	b.mu.Lock()
	watched := len(b.subscribers) > 0
	b.mu.Unlock()
	if !watched {
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	events, err := b.reader.Read(ctx, touched)
	if err != nil {
		b.logger.LogAttrs(ctx, slog.LevelError, "failed to read the rows a change touched", logging.Err(err)...)
		b.restore(touched)

		return false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, event := range events {
		for subscription := range b.subscribers {
			b.send(ctx, subscription, event)
		}
	}

	return true
}

// take empties the touched set and returns it with each kind's ids in order.
func (b *Broker) take() Touched {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	touched := Touched{
		Agents:    slices.Sorted(maps.Keys(b.pending[KindAgent])),
		Workflows: slices.Sorted(maps.Keys(b.pending[KindWorkflow])),
		Schedules: slices.Sorted(maps.Keys(b.pending[KindSchedule])),
	}
	clear(b.pending)

	return touched
}

// restore puts the ids a failed render took back for the next one.
func (b *Broker) restore(touched Touched) {
	b.Touch(store.TableAgents, touched.Agents...)
	b.Touch(store.TableWorkflows, touched.Workflows...)
	b.Touch(store.TableSchedules, touched.Schedules...)
}

// Subscribe opens a subscription that starts with a snapshot of every row, then every change
// for as long as it stays open.
func (b *Broker) Subscribe(ctx context.Context) (*Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed || b.reader == nil {
		return nil, ErrClosed
	}

	// Nothing renders while the lock is held, so no change can land between the snapshot and the
	// subscription's place in the fan-out.
	readCtx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	snapshot, err := b.reader.Snapshot(readCtx)
	if err != nil {
		return nil, err
	}

	subscription := &Subscription{broker: b, events: make(chan Event, subscriberBuffer), snapshot: snapshot}
	b.subscribers[subscription] = struct{}{}

	return subscription, nil
}

// send hands one event to a subscriber, dropping the subscriber when it has fallen too far
// behind to take it.
func (b *Broker) send(ctx context.Context, subscription *Subscription, event Event) {
	select {
	case subscription.events <- event:
	default:
		b.logger.LogAttrs(ctx, slog.LevelWarn, "dropped a watcher that fell behind the stream")
		b.drop(subscription)
	}
}

func (b *Broker) drop(subscription *Subscription) {
	if _, open := b.subscribers[subscription]; !open {
		return
	}

	delete(b.subscribers, subscription)
	close(subscription.events)
}

// Close ends every subscription and stops the render loop, dropping whatever was touched and
// not yet rendered.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for subscription := range b.subscribers {
		b.drop(subscription)
	}

	b.pendingMu.Lock()
	b.pending = nil
	close(b.wake)
	b.pendingMu.Unlock()
}

// Subscription is one watcher: the snapshot it opened with, and the stream of changes after it.
type Subscription struct {
	broker   *Broker
	events   chan Event
	snapshot Snapshot
}

// Snapshot is every row the listings held as the subscription opened.
func (s *Subscription) Snapshot() Snapshot {
	return s.snapshot
}

// Events is the stream of changes since the snapshot, closed once the subscription ends.
func (s *Subscription) Events() <-chan Event {
	return s.events
}

// Close ends the subscription, after which Events drains and closes.
func (s *Subscription) Close() {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()

	s.broker.drop(s)
}
