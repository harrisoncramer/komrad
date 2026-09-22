package watch

import (
	"testing"
	"time"

	"weave/db"
	"weave/internal/dbtest"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listened is a broker listening to a database of the test's own, with the pool a test writes
// through.
type listened struct {
	broker *Broker
	pool   *pgxpool.Pool
}

func newListened(t *testing.T, stores *fakeStores) *listened {
	t.Helper()

	l := newPooled(t)
	l.listen(t, stores)

	return l
}

// newPooled opens a pool on a database of the test's own, with nothing listening to it yet, so
// a write made now raises a notification nobody hears.
func newPooled(t *testing.T) *listened {
	t.Helper()

	pool, err := pgxpool.New(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return &listened{pool: pool}
}

// listen puts the broker on the watch channel.
func (l *listened) listen(t *testing.T, stores *fakeStores) {
	t.Helper()

	l.broker = NewBroker(NewBrokerParams{})
	require.NoError(t, l.broker.Start(l.pool, newRows(stores)))
	t.Cleanup(l.broker.Close)
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
	stores := newStores()
	l := newListened(t, stores)

	subscription, err := l.broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow("a"))
	l.notify(t, "agents a")
	change := receive(t, subscription)
	require.Equal(t, "a", change.Key)
	require.NotNil(t, change.Row.GetAgent())
}

// TestALostConnectionDropsEveryWatcher pins the answer to a connection going: every
// subscription is closed, none opens until the listener is back, and the one that opens then
// hears what is raised after.
func TestALostConnectionDropsEveryWatcher(t *testing.T) {
	stores := newStores()
	l := newListened(t, stores)

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

	stores.agents.put(agentRow("b"))
	l.notify(t, "agents b")
	require.Equal(t, "b", receive(t, again).Key)
}

// seedAgent writes a workflow and an agent in it, and answers the agent's id.
func (l *listened) seedAgent(t *testing.T) string {
	t.Helper()

	workflow, config, provisioner, agent := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	l.exec(t, `INSERT INTO workflow_configs (id, harness, cwd, flags) VALUES ($1, 'claude', '/repo', '')`, config)
	l.exec(t, `INSERT INTO workflow_provisioner_configs (id, worktree_enabled, worktree_base_branch, worktree_dir, worktree_copy_paths, worktree_symlink_paths, worktree_diff_exclude)
		VALUES ($1, false, 'main', '', '{}', '{}', '{}')`, provisioner)
	l.exec(t, `INSERT INTO workflows (id, name, context, workflow_config_id, provisioner_config_id) VALUES ($1, 'flow', '', $2, $3)`, workflow, config, provisioner)
	l.exec(t, `INSERT INTO agents (id, name, workflow_id, branch) VALUES ($1, 'agent', $2, 'agent/x')`, agent, workflow)

	return agent
}

func (l *listened) exec(t *testing.T, sql string, args ...any) {
	t.Helper()

	_, err := l.pool.Exec(t.Context(), sql, args...)
	require.NoError(t, err)
}

// TestAWriteReachesTheWatchersUnderItsResourceKey pins the routing the triggers do, end to end
// through a real database: a write to a resource's own table reaches a watcher under that row's
// key, and a write to a table projected onto it reaches the watcher under the parent's key.
func TestAWriteReachesTheWatchersUnderItsResourceKey(t *testing.T) {
	stores := newStores()
	l := newPooled(t)

	// The seed is written before anything listens, so only the writes below raise anything.
	agent := l.seedAgent(t)
	l.listen(t, stores)

	subscription, err := l.broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.agents.put(agentRow(agent))
	l.exec(t, `UPDATE agents SET name = 'renamed' WHERE id = $1`, agent)
	require.Equal(t, agent, receive(t, subscription).Key, "a write to the resource's own table")

	moved := agentRow(agent)
	moved.Name = "moved"
	stores.agents.put(moved)
	l.exec(t, `INSERT INTO agent_metadata (agent_id, key, value) VALUES ($1, 'branch', 'agent/x')`, agent)

	change := receive(t, subscription)
	assert.Equal(t, agent, change.Key, "a write to a projected table reaches the watcher under its parent's key")
	assert.Equal(t, watchv1.Resource_RESOURCE_AGENT, change.Resource)
}
