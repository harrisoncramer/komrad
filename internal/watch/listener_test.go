package watch

import (
	"testing"
	"time"

	"weave/db"
	"weave/internal/dbtest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// listened is a broker listening to a database of the test's own, with the pool a test writes
// through.
type listened struct {
	broker *Broker
	pool   *pgxpool.Pool
}

func newListened(t *testing.T, reader *fakeReader) *listened {
	t.Helper()

	pool, err := pgxpool.New(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	broker := NewBroker(NewBrokerParams{})
	require.NoError(t, broker.Start(pool, reader))
	t.Cleanup(broker.Close)

	return &listened{broker: broker, pool: pool}
}

// notify raises a payload on the watch channel the way a trigger would.
func (l *listened) notify(t *testing.T, payload string) {
	t.Helper()

	_, err := l.pool.Exec(t.Context(), "SELECT pg_notify($1, $2)", db.WatchChannel, payload)
	require.NoError(t, err)
}

// killListener ends the listener's connection from the server's side, finding it as the
// backend on this test's own database whose last statement was the LISTEN.
func (l *listened) killListener(t *testing.T) {
	t.Helper()

	_, err := l.pool.Exec(t.Context(), `
		SELECT
			pg_terminate_backend(pid)
		FROM
			pg_stat_activity
		WHERE
			datname = current_database()
			AND pid <> pg_backend_pid()
			AND query = 'LISTEN ' || $1`, db.WatchChannel)
	require.NoError(t, err)
}

// TestANotificationReachesTheWatchers pins the listener end to end: a notification raised in
// the database is read back and reaches a subscription as its row.
func TestANotificationReachesTheWatchers(t *testing.T) {
	l := newListened(t, &fakeReader{})

	subscription, err := l.broker.Subscribe(t.Context())
	require.NoError(t, err)

	l.notify(t, "agents a")
	change := receive(t, subscription)
	require.Equal(t, "a", change.Key)
	require.NotNil(t, change.Row.GetAgent())
}

// TestALostConnectionDropsEveryWatcher pins the answer to a connection going: every
// subscription is closed, none opens until the listener is back, and the one that opens then
// hears what is raised after.
func TestALostConnectionDropsEveryWatcher(t *testing.T) {
	l := newListened(t, &fakeReader{})

	subscription, err := l.broker.Subscribe(t.Context())
	require.NoError(t, err)

	l.killListener(t)

	select {
	case _, open := <-subscription.Changes():
		require.False(t, open, "the subscription closes with the connection")
	case <-time.After(5 * time.Second):
		t.Fatal("the subscription outlived the connection")
	}

	_, err = l.broker.Subscribe(t.Context())
	require.ErrorIs(t, err, ErrNotListening, "no snapshot is taken over the gap")

	var again *Subscription
	require.Eventually(t, func() bool {
		again, err = l.broker.Subscribe(t.Context())

		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "the listener dials again")

	l.notify(t, "agents b")
	require.Equal(t, "b", receive(t, again).Key)
}
