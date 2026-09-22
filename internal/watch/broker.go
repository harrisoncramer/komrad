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
	"weave/internal/resource"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/jackc/pgx/v5/pgxpool"
)

// subscriberBuffer is how many live events a watcher may fall behind before it is dropped.
const subscriberBuffer = 1024

// readTimeout is how long one read-back or snapshot may take.
const readTimeout = 10 * time.Second

// retryAfter is how long a render that failed to read waits before the next one.
const retryAfter = time.Second

// ErrClosed says the broker has stopped and takes no more watchers.
var ErrClosed = fail.Precondition("the watch broker is closed")

// ErrNotListening says the broker has no connection hearing the database, so a snapshot taken
// now could miss a change.
var ErrNotListening = fail.Precondition("the watch is not listening to the database")

// resources is the resource each table a notification names is the rows of.
var resources = resourcesByTable()

// resourcesByTable reads the annotated resources into the table each one is keyed by on the
// wire.
func resourcesByTable() map[string]watchv1.Resource {
	byTable := map[string]watchv1.Resource{}
	for _, r := range resource.All() {
		byTable[r.Option.GetTable()] = watchv1.Resource(watchv1.Resource_value[r.EnumValue()])
	}

	return byTable
}

// Touched is the keys one render reads back, by resource, each resource's in order.
type Touched map[watchv1.Resource][]string

// Event is one row as a watcher should now hold it, or a tombstone for one it should drop.
type Event struct {
	Resource watchv1.Resource
	Key      string
	// Row carries the row, and nil is a tombstone.
	Row *watchv1.Row
}

// Reader reads the rows a render names back, and every row for a snapshot.
type Reader interface {
	// Read renders each touched key as its row, or as a tombstone when its watched set no
	// longer holds it.
	Read(ctx context.Context, touched Touched) ([]Event, error)
	// Snapshot renders every row the watched sets hold.
	Snapshot(ctx context.Context) ([]*watchv1.Row, error)
}

// NewBrokerParams is what a broker is built over.
type NewBrokerParams struct {
	Logger *slog.Logger
}

// Broker hears from the database which rows moved, reads them back and fans them out to every
// open subscription.
type Broker struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
	reader Reader

	// pendingMu guards what has been heard and not yet read.
	pendingMu sync.Mutex
	pending   map[watchv1.Resource]map[string]struct{}
	// wake has room for one poke, which is all the loop needs to know.
	wake chan struct{}

	// mu guards the subscribers, whether a listener is up and whether the broker has closed.
	mu          sync.Mutex
	subscribers map[*Subscription]struct{}
	listening   bool
	listenerPID uint32
	closed      bool

	// stop ends the listener, and stopped is closed once it has let its connection go.
	stop    context.CancelFunc
	stopped chan struct{}
}

// NewBroker builds the broker, which hears nothing and renders nothing until Start.
func NewBroker(params NewBrokerParams) *Broker {
	logger := params.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	// A broker never started has no listener to wait for.
	stopped := make(chan struct{})
	close(stopped)

	return &Broker{
		logger:      logger,
		pending:     map[watchv1.Resource]map[string]struct{}{},
		wake:        make(chan struct{}, 1),
		subscribers: map[*Subscription]struct{}{},
		stop:        func() {},
		stopped:     stopped,
	}
}

// Start puts a connection from the pool on the watch channel and begins rendering through the
// reader, both of which Close ends, failing when the database cannot be listened to.
func (b *Broker) Start(pool *pgxpool.Pool, reader Reader) error {
	b.pool = pool

	ctx, cancel := context.WithCancel(context.Background())
	conn, err := b.connect(ctx)
	if err != nil {
		cancel()

		return err
	}

	b.serve(reader)
	b.stop = cancel
	b.stopped = make(chan struct{})
	go func() {
		defer close(b.stopped)

		b.listen(ctx, conn)
	}()

	return nil
}

// serve begins rendering what is heard through the reader.
func (b *Broker) serve(reader Reader) {
	b.reader = reader

	go func() {
		for range b.wake {
			if !b.render() {
				time.Sleep(retryAfter)
			}
		}
	}()
}

// touch says the rows of a table with these keys may have moved.
func (b *Broker) touch(table string, keys ...string) {
	res, ok := resources[table]
	if !ok {
		b.logger.LogAttrs(context.Background(), slog.LevelWarn, "a notification named a table no resource is a row of", slog.String(logging.WatchTable, table))

		return
	}

	b.touchResource(res, keys)
}

func (b *Broker) touchResource(res watchv1.Resource, keys []string) {
	if len(keys) == 0 {
		return
	}

	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	if b.pending == nil {
		return
	}

	touched := b.pending[res]
	if touched == nil {
		touched = map[string]struct{}{}
		b.pending[res] = touched
	}
	for _, key := range keys {
		touched[key] = struct{}{}
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

// take empties the pending set and returns it with each resource's keys in order.
func (b *Broker) take() Touched {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()

	touched := Touched{}
	for res, keys := range b.pending {
		touched[res] = slices.Sorted(maps.Keys(keys))
	}
	clear(b.pending)

	return touched
}

// restore puts the keys a failed render took back for the next one.
func (b *Broker) restore(touched Touched) {
	for res, keys := range touched {
		b.touchResource(res, keys)
	}
}

// Subscribe opens a subscription that starts with a snapshot of every row, then every change
// for as long as it stays open.
func (b *Broker) Subscribe(ctx context.Context) (*Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil, ErrClosed
	}
	if !b.listening {
		return nil, ErrNotListening
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

// Close ends every subscription, lets the listener's connection go and stops the render loop,
// dropping whatever was heard and not yet rendered.
func (b *Broker) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()

		return
	}
	b.closed = true

	for subscription := range b.subscribers {
		b.drop(subscription)
	}
	b.mu.Unlock()

	// The listener closes its own connection, and takes the lock to say so, so it is waited for
	// with the lock let go.
	b.stop()
	<-b.stopped

	b.pendingMu.Lock()
	b.pending = nil
	close(b.wake)
	b.pendingMu.Unlock()
}

// Subscription is one watcher: the snapshot it opened with, and the stream of changes after it.
type Subscription struct {
	broker   *Broker
	events   chan Event
	snapshot []*watchv1.Row
}

// Snapshot is every row the watched sets held as the subscription opened.
func (s *Subscription) Snapshot() []*watchv1.Row {
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
