package watch

import (
	"testing"
	"time"

	"weave/db"
	"weave/internal/dbtest"
	notifystore "weave/internal/store/notify"
	workflowstore "weave/internal/store/workflow"
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

	l.broker = NewBroker(NewBrokerParams{Pool: l.pool, Rows: newRows(stores)})
	require.NoError(t, l.broker.Start())
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
			AND query LIKE '%LISTEN ' || $1 || '%'`, db.WatchChannel)
	require.NoError(t, err)
}

// TestListenIsOnTheChannelTheTriggersRaise pins that the connection ends up on db.WatchChannel.
func TestListenIsOnTheChannelTheTriggersRaise(t *testing.T) {
	l := newPooled(t)

	conn, err := l.pool.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()

	require.NoError(t, notifystore.NewNotifyStore(conn).OpenWatchChannel(t.Context()))

	var listening string
	require.NoError(t, conn.QueryRow(t.Context(), "SELECT pg_listening_channels()").Scan(&listening))
	assert.Equal(t, db.WatchChannel, listening)
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

	return l.seed(t).agent
}

// seeded is the rows a lookup projection has to find its way between.
type seeded struct {
	workflow   string
	config     string
	agent      string
	repository string
}

// seed writes a workflow with its config and an agent in it, on a repository of its own.
func (l *listened) seed(t *testing.T) seeded {
	t.Helper()

	rows := seeded{
		workflow:   uuid.NewString(),
		config:     uuid.NewString(),
		agent:      uuid.NewString(),
		repository: "https://example.test/" + uuid.NewString(),
	}
	provisioner := uuid.NewString()
	l.exec(t, `INSERT INTO workflow_configs (id, harness, cwd, flags) VALUES ($1, 'claude', '/repo', '')`, rows.config)
	l.exec(t, `INSERT INTO workflow_provisioner_configs (id, worktree_enabled, worktree_base_branch, worktree_dir, worktree_copy_paths, worktree_symlink_paths, worktree_diff_exclude)
		VALUES ($1, false, 'main', '', '{}', '{}', '{}')`, provisioner)
	l.exec(t, `INSERT INTO workflows (id, name, context, workflow_config_id, provisioner_config_id) VALUES ($1, 'flow', '', $2, $3)`, rows.workflow, rows.config, provisioner)
	l.exec(t, `INSERT INTO agents (id, name, workflow_id, branch, repository_url) VALUES ($1, 'agent', $2, 'agent/x', $3)`, rows.agent, rows.workflow, rows.repository)

	return rows
}

// TestALookupProjectionReachesItsParents pins the two lookup projections against a real
// database: a write to a table that holds no parent key reaches the watcher under the key of
// the parent row that matches it.
func TestALookupProjectionReachesItsParents(t *testing.T) {
	stores := newStores()
	l := newPooled(t)

	// The seed is written before anything listens, so only the writes below raise anything.
	rows := l.seed(t)
	l.listen(t, stores)

	subscription, err := l.broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.workflows.put(workflowstore.Workflow{ID: rows.workflow, Name: "flow"})
	stores.agents.put(agentRow(rows.agent))

	// workflow_configs holds no workflow id: its parent is the workflow whose
	// workflow_config_id is this row's id.
	l.exec(t, `UPDATE workflow_configs SET flags = '--model opus' WHERE id = $1`, rows.config)

	change := receive(t, subscription)
	assert.Equal(t, watchv1.Resource_RESOURCE_WORKFLOW, change.Resource)
	assert.Equal(t, rows.workflow, change.Key, "a workflow config reaches its workflow")

	// pull_request_syncs holds no agent id: its parents are the live agents on its repository.
	l.exec(t, `INSERT INTO pull_request_syncs (repository_url, updated_through) VALUES ($1, now())`, rows.repository)

	change = receive(t, subscription)
	assert.Equal(t, watchv1.Resource_RESOURCE_AGENT, change.Resource)
	assert.Equal(t, rows.agent, change.Key, "a repository sync reaches the live agents on that repository")
}

// TestALookupProjectionSkipsAnArchivedParent pins the narrowing: an archived agent is not a
// parent, so a sync on its repository reaches nobody.
func TestALookupProjectionSkipsAnArchivedParent(t *testing.T) {
	stores := newStores()
	l := newPooled(t)

	rows := l.seed(t)
	l.exec(t, `UPDATE agents SET archived_at = now() WHERE id = $1`, rows.agent)
	l.listen(t, stores)

	subscription, err := l.broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.workflows.put(workflowstore.Workflow{ID: rows.workflow, Name: "flow"})
	stores.agents.put(agentRow(rows.agent))

	l.exec(t, `INSERT INTO pull_request_syncs (repository_url, updated_through) VALUES ($1, now())`, rows.repository)
	// A write that reaches no parent raises nothing, so the workflow write is what proves the
	// stream is still live.
	l.exec(t, `UPDATE workflow_configs SET flags = '--model opus' WHERE id = $1`, rows.config)

	change := receive(t, subscription)
	assert.Equal(t, watchv1.Resource_RESOURCE_WORKFLOW, change.Resource, "the archived agent's sync raised nothing")
	assert.Equal(t, rows.workflow, change.Key)
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
