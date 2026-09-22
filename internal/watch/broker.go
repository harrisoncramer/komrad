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

// DefaultCoalesce is how long touches are gathered before one render reads them all back.
const DefaultCoalesce = 50 * time.Millisecond

// subscriberBuffer is how many live events a watcher may fall behind before it is dropped.
const subscriberBuffer = 1024

// readTimeout is how long one read-back or snapshot may take.
const readTimeout = 10 * time.Second

// retryAfter is how long a render that failed to read waits before the next one tries again.
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

// Touched is the ids one render reads back, by kind.
type Touched struct {
	Agents    []string
	Workflows []string
	Schedules []string
}

// Event is one row as a watcher should now hold it, a tombstone for one it should drop, or the
// mark that a snapshot is complete.
type Event struct {
	Seq  uint64
	Kind Kind
	ID   string
	// Agent, Workflow and Schedule carry the row, and all three nil on a row event is a tombstone.
	Agent    *agentv1.Agent
	Workflow *workflowv1.Workflow
	Schedule *schedulev1.Schedule
	// Synced says the snapshot is complete and live events follow.
	Synced bool
}

// IsTombstone reports whether the event says the row is gone from its listing.
func (e Event) IsTombstone() bool {
	return !e.Synced && e.Agent == nil && e.Workflow == nil && e.Schedule == nil
}

// Reader reads the rows a render names back, and every row for a snapshot.
type Reader interface {
	// Read renders each touched id as its row, or as a tombstone when its listing no longer
	// holds it.
	Read(ctx context.Context, touched Touched) ([]Event, error)
	// Snapshot renders every row the listings hold.
	Snapshot(ctx context.Context) ([]Event, error)
}

// NewBrokerParams is what a broker is built over.
type NewBrokerParams struct {
	Logger *slog.Logger
	// Coalesce is how long touches are gathered before a render, DefaultCoalesce when zero.
	Coalesce time.Duration
}

// Broker takes touches from every writer, coalesces them, reads the rows back and fans them out
// to every open subscription.
type Broker struct {
	reader   Reader
	logger   *slog.Logger
	coalesce time.Duration

	mu      sync.Mutex
	pending map[Kind]map[string]struct{}

	wake         chan struct{}
	subscribes   chan subscribeRequest
	unsubscribes chan *Subscription
	stop         chan struct{}
	stopped      chan struct{}
	closeOnce    sync.Once

	// seq and subscribers belong to the render goroutine alone.
	seq         uint64
	subscribers map[*Subscription]struct{}
}

// NewBroker builds the broker, which takes touches at once and renders none of them until
// Start names what reads the rows back.
func NewBroker(params NewBrokerParams) *Broker {
	coalesce := params.Coalesce
	if coalesce == 0 {
		coalesce = DefaultCoalesce
	}

	logger := params.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &Broker{
		logger:       logger,
		coalesce:     coalesce,
		pending:      map[Kind]map[string]struct{}{},
		wake:         make(chan struct{}, 1),
		subscribes:   make(chan subscribeRequest),
		unsubscribes: make(chan *Subscription),
		stop:         make(chan struct{}),
		stopped:      make(chan struct{}),
		subscribers:  map[*Subscription]struct{}{},
	}
}

type subscribeRequest struct {
	reply chan subscribeReply
}

type subscribeReply struct {
	subscription *Subscription
	err          error
}

// Start begins rendering through the reader, which Close ends.
func (b *Broker) Start(reader Reader) {
	b.reader = reader

	go b.run()
}

// kinds is the listing each table a store names belongs to.
var kinds = map[string]Kind{
	store.TableAgents:    KindAgent,
	store.TableWorkflows: KindWorkflow,
	store.TableSchedules: KindSchedule,
}

// Touch says the rows of a listing with these ids may have moved.
func (b *Broker) Touch(table string, ids ...string) {
	kind, ok := kinds[table]
	if !ok {
		b.logger.LogAttrs(context.Background(), slog.LevelWarn, "a touch named a table no listing is keyed by", slog.String(logging.WatchTable, table))

		return
	}

	b.touch(kind, ids)
}

func (b *Broker) touch(kind Kind, ids []string) {
	if len(ids) == 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	touched := b.pending[kind]
	if touched == nil {
		touched = map[string]struct{}{}
		b.pending[kind] = touched
	}
	for _, id := range ids {
		touched[id] = struct{}{}
	}

	select {
	case b.wake <- struct{}{}:
	default:
	}
}

// takePending empties the touched set and returns it with each kind's ids in order.
func (b *Broker) takePending() Touched {
	b.mu.Lock()
	defer b.mu.Unlock()

	touched := Touched{
		Agents:    slices.Sorted(maps.Keys(b.pending[KindAgent])),
		Workflows: slices.Sorted(maps.Keys(b.pending[KindWorkflow])),
		Schedules: slices.Sorted(maps.Keys(b.pending[KindSchedule])),
	}
	clear(b.pending)

	return touched
}

// Subscribe opens a subscription that starts with a snapshot of every row, then Synced, then
// every change for as long as it stays open, giving up when ctx ends before the snapshot is
// taken.
func (b *Broker) Subscribe(ctx context.Context) (*Subscription, error) {
	req := subscribeRequest{reply: make(chan subscribeReply, 1)}

	select {
	case b.subscribes <- req:
	case <-b.stopped:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case reply := <-req.reply:
		return reply.subscription, reply.err
	case <-b.stopped:
		return nil, ErrClosed
	}
}

// Close ends every subscription and stops the render goroutine, dropping whatever was touched
// and not yet rendered.
func (b *Broker) Close() {
	b.closeOnce.Do(func() {
		close(b.stop)
		if b.reader == nil {
			close(b.stopped)
		}
	})
	<-b.stopped
}

func (b *Broker) run() {
	ctx := context.Background()
	defer close(b.stopped)
	defer logging.LogAndRepanic(ctx)

	var window <-chan time.Time
	for {
		select {
		case <-b.wake:
			if window == nil {
				window = time.After(b.coalesce)
			}
		case <-window:
			window = nil
			if !b.render(ctx) {
				window = time.After(retryAfter)
			}
		case req := <-b.subscribes:
			req.reply <- b.subscribe(ctx)
		case subscription := <-b.unsubscribes:
			b.drop(subscription)
		case <-b.stop:
			for subscription := range b.subscribers {
				b.drop(subscription)
			}

			return
		}
	}
}

// render reads back everything touched since the last one and sends it to every subscriber,
// reporting false when the read failed and the ids are back in the pending set.
func (b *Broker) render(ctx context.Context) bool {
	touched := b.takePending()
	if len(b.subscribers) == 0 {
		return true
	}

	readCtx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	events, err := b.reader.Read(readCtx, touched)
	if err != nil {
		b.logger.LogAttrs(ctx, slog.LevelError, "failed to read the rows a change touched", logging.Err(err)...)
		b.restore(touched)

		return false
	}

	for _, event := range events {
		b.seq++
		event.Seq = b.seq
		for subscription := range b.subscribers {
			b.send(ctx, subscription, event)
		}
	}

	return true
}

// restore puts the ids a failed render took back into the pending set for the next one.
func (b *Broker) restore(touched Touched) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for kind, ids := range map[Kind][]string{KindAgent: touched.Agents, KindWorkflow: touched.Workflows, KindSchedule: touched.Schedules} {
		for _, id := range ids {
			pending := b.pending[kind]
			if pending == nil {
				pending = map[string]struct{}{}
				b.pending[kind] = pending
			}
			pending[id] = struct{}{}
		}
	}
}

// subscribe registers a watcher and hands it the snapshot, stamped with the seq of the last
// render.
func (b *Broker) subscribe(ctx context.Context) subscribeReply {
	readCtx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	rows, err := b.reader.Snapshot(readCtx)
	if err != nil {
		return subscribeReply{err: err}
	}

	subscription := &Subscription{
		broker: b,
		events: make(chan Event, len(rows)+1+subscriberBuffer),
	}
	for _, event := range rows {
		event.Seq = b.seq
		subscription.events <- event
	}
	subscription.events <- Event{Seq: b.seq, Synced: true}
	b.subscribers[subscription] = struct{}{}

	return subscribeReply{subscription: subscription}
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

// Subscription is one watcher's stream of events.
type Subscription struct {
	broker *Broker
	events chan Event
	once   sync.Once
}

// Events is the stream, closed once the subscription ends.
func (s *Subscription) Events() <-chan Event {
	return s.events
}

// Close ends the subscription, after which Events drains and closes.
func (s *Subscription) Close() {
	s.once.Do(func() {
		select {
		case s.broker.unsubscribes <- s:
		case <-s.broker.stopped:
		}
	})
}
