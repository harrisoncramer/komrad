package watch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"weave/internal/fail"
	"weave/internal/logging"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
)

// subscriberBuffer is how many live changes a watcher may fall behind before it is dropped.
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

// Change is one row as a watcher should now hold it, or a tombstone for one it should drop.
type Change struct {
	Resource watchv1.Resource
	Key      string
	// Row carries the row, and nil is a tombstone.
	Row *watchv1.Row
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
	rows   *Rows

	// pendingMu guards what has been heard and not yet read.
	pendingMu sync.Mutex
	pending   map[watchv1.Resource]map[string]struct{}
	// wake has room for one poke, which is all the loop needs to know.
	wake chan struct{}

	// mu guards the subscribers, the rows last sent, whether a listener is up and whether the
	// broker has closed.
	mu          sync.Mutex
	subscribers map[*Subscription]struct{}
	sent        map[watchv1.Resource]map[string]*watchv1.Row
	listening   bool
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
		sent:        map[watchv1.Resource]map[string]*watchv1.Row{},
		stop:        func() {},
		stopped:     stopped,
	}
}

// Start puts a connection from the pool on the watch channel and begins rendering through the
// rows, both of which Close ends, failing when the database cannot be listened to.
func (b *Broker) Start(pool *pgxpool.Pool, rows *Rows) error {
	b.pool = pool

	ctx, cancel := context.WithCancel(context.Background())
	conn, err := b.connect(ctx)
	if err != nil {
		cancel()

		return err
	}

	b.serve(rows)
	b.stop = cancel
	b.stopped = make(chan struct{})
	go func() {
		defer close(b.stopped)

		b.listen(ctx, conn)
	}()

	return nil
}

// serve begins rendering what is heard through the rows.
func (b *Broker) serve(rows *Rows) {
	b.rows = rows

	go func() {
		for range b.wake {
			if !b.render() {
				time.Sleep(retryAfter)
			}
		}
	}()
}

// render reads back everything touched since the last one and sends what moved to every
// subscriber, reporting false when the read failed and the ids are back in the pending set.
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

	changes, err := b.rows.Read(ctx, touched)
	if err != nil {
		b.logger.LogAttrs(ctx, slog.LevelError, "failed to read the rows a change touched", logging.Err(err)...)
		b.restore(touched)

		return false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, change := range changes {
		if !b.moved(change) {
			continue
		}

		for subscription := range b.subscribers {
			b.send(ctx, subscription, change)
		}
	}

	return true
}

// moved reports whether a change says anything the last one for its key did not, recording it
// as the row the stream now holds.
func (b *Broker) moved(change Change) bool {
	held := b.sent[change.Resource]

	if change.Row == nil {
		delete(held, change.Key)

		return true
	}
	if proto.Equal(held[change.Key], change.Row) {
		return false
	}

	if held == nil {
		held = map[string]*watchv1.Row{}
		b.sent[change.Resource] = held
	}
	held[change.Key] = change.Row

	return true
}

// reseed makes a snapshot the rows the stream holds.
func (b *Broker) reseed(snapshot []Change) {
	b.sent = map[watchv1.Resource]map[string]*watchv1.Row{}
	for _, change := range snapshot {
		held := b.sent[change.Resource]
		if held == nil {
			held = map[string]*watchv1.Row{}
			b.sent[change.Resource] = held
		}
		held[change.Key] = change.Row
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

	snapshot, err := b.rows.Snapshot(readCtx)
	if err != nil {
		return nil, err
	}
	// Explainer: only the first watcher reseeds. While one is open the memory is already what
	// went out, and a later snapshot can be newer than a render still in flight. Seeding from
	// that snapshot would make the render's row compare equal and be dropped, so the watchers
	// already on the stream would never receive it, with no notification left pending to correct
	// them.
	if len(b.subscribers) == 0 {
		b.reseed(snapshot)
	}

	rows := xslice.Map(snapshot, func(change Change) *watchv1.Row { return change.Row })
	subscription := &Subscription{broker: b, changes: make(chan Change, subscriberBuffer), snapshot: rows}
	b.subscribers[subscription] = struct{}{}

	return subscription, nil
}

// send hands one change to a subscriber, dropping the subscriber when it has fallen too far
// behind to take it.
func (b *Broker) send(ctx context.Context, subscription *Subscription, change Change) {
	select {
	case subscription.changes <- change:
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
	close(subscription.changes)

	// Nothing holds the stream's rows once nobody is on it, and the next watcher reseeds from
	// its own snapshot.
	if len(b.subscribers) == 0 {
		clear(b.sent)
	}
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
	changes  chan Change
	snapshot []*watchv1.Row
}

// Snapshot is every row the watched sets held as the subscription opened.
func (s *Subscription) Snapshot() []*watchv1.Row {
	return s.snapshot
}

// Changes is the stream of changes since the snapshot, closed once the subscription ends.
func (s *Subscription) Changes() <-chan Change {
	return s.changes
}

// Close ends the subscription, after which Changes drains and closes.
func (s *Subscription) Close() {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()

	s.broker.drop(s)
}
