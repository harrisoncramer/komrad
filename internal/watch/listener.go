package watch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"komrad/internal/logging"
	notifystore "komrad/internal/store/notify"

	"github.com/jackc/pgx/v5"
)

const (
	// listenTimeout is how long one wait for a notification runs before the connection is
	// pinged, which is what finds a half-open socket.
	listenTimeout = 30 * time.Second
	// listenRetry is the wait before the listener dials again, doubling each time up to
	// listenMaxRetry.
	listenRetry    = time.Second
	listenMaxRetry = 15 * time.Second
)

// listen holds the connection on the watch channel for the life of ctx, dialling again after
// every loss.
func (b *Broker) listen(ctx context.Context, conn *pgx.Conn) {
	for conn != nil {
		err := b.hold(ctx, conn)
		if ctx.Err() != nil {
			return
		}

		b.logger.LogAttrs(ctx, slog.LevelError, "lost the database's notifications", logging.Err(err)...)
		b.lost()

		conn = b.reconnect(ctx)
	}
}

// reconnect dials until a connection is on the channel, waiting longer after each failure, and
// answers nil once ctx has ended.
func (b *Broker) reconnect(ctx context.Context) *pgx.Conn {
	wait := listenRetry

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}

		conn, err := b.connect(ctx)
		if err == nil {
			return conn
		}

		b.logger.LogAttrs(ctx, slog.LevelError, "failed to listen to the database again", logging.Err(err)...)
		wait = min(wait*2, listenMaxRetry)
	}
}

// connect takes one connection out of the pool for the listener alone and puts it on the
// watch channel.
func (b *Broker) connect(ctx context.Context) (*pgx.Conn, error) {
	pooled, err := b.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to take a connection to listen on: %w", err)
	}
	// A pool hands the same connection to whoever asks next, and a LISTEN on it would go with
	// it.
	conn := pooled.Hijack()

	if err := notifystore.NewNotifyStore(conn).OpenWatchChannel(ctx); err != nil {
		_ = conn.Close(ctx)

		return nil, err
	}
	b.up()

	return conn, nil
}

// hold listens on the connection until it is lost or ctx ends, and closes it.
func (b *Broker) hold(ctx context.Context, conn *pgx.Conn) error {
	defer func() { _ = conn.Close(ctx) }()

	for {
		waitCtx, cancel := context.WithTimeout(ctx, listenTimeout)
		notification, err := conn.WaitForNotification(waitCtx)
		cancel()

		switch {
		case err == nil:
			b.handleNotification(notification.Payload)
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, context.DeadlineExceeded):
			// A wait that ran out leaves the connection open, so a ping is what says whether
			// the server is still there.
			pingCtx, cancel := context.WithTimeout(ctx, listenTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return fmt.Errorf("failed to ping the listening connection: %w", err)
			}
		default:
			return fmt.Errorf("failed to wait for a notification: %w", err)
		}
	}
}

// handleNotification signals the row a notification names.
func (b *Broker) handleNotification(payload string) {
	table, key, found := strings.Cut(payload, " ")
	if !found {
		b.logger.LogAttrs(context.Background(), slog.LevelWarn, "a notification carried no key", slog.String(logging.WatchTable, payload))

		return
	}

	resource, ok := b.tables[table]
	if !ok {
		if b.shouldWarn("table " + table) {
			b.logger.LogAttrs(context.Background(), slog.LevelWarn, "a notification named a table no resource is a row of", slog.String(logging.WatchTable, table))
		}

		return
	}

	b.signal(resource, key)
}

// up says the listener is on the channel, so watchers may open.
func (b *Broker) up() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.listening = true
}

// lost says the listener's connection has gone and drops every watcher, whose snapshots are
// only good while no change is missed.
func (b *Broker) lost() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.listening = false
	for subscription := range b.subscribers {
		b.drop(subscription)
	}
}
