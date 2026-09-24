package watch

import (
	"context"
	"testing"
	"time"

	"weave/db/testmodels"
	"weave/internal/dbtest"
	"weave/internal/store"
	notifystore "weave/internal/store/notify"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testResources maps the tables the tests notify on to their resources.
var testResources = []Resource{
	{
		Enum:  watchv1.Resource_RESOURCE_AGENT,
		Table: "agents",
	},
	{
		Enum:  watchv1.Resource_RESOURCE_WORKFLOW,
		Table: "workflows",
	},
}

// listened is a broker listening to a database of the test's own, with the pool a test writes
// through.
type listened struct {
	broker   *Broker
	pool     *pgxpool.Pool
	ownerURL string
}

func newListened(t *testing.T, stores *fakeStores) *listened {
	t.Helper()

	l := newPooled(t)
	l.listen(t, stores)

	return l
}

// newPooled opens a pool on a database of the test's own, with nothing listening to it yet, so
// a write made now raises a notification nobody receives.
func newPooled(t *testing.T) *listened {
	t.Helper()

	database := dbtest.New(t)
	st, err := store.Open(t.Context(), dbtest.NewEngine(t, database).URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	return &listened{
		pool:     st.GetRawPool(),
		ownerURL: database.OwnerURL,
	}
}

// listen puts the broker on the watch channel.
func (l *listened) listen(t *testing.T, stores *fakeStores) {
	t.Helper()

	l.broker = NewBroker(NewBrokerParams{
		Pool:      l.pool,
		Rows:      newRows(stores),
		Resources: testResources,
	})
	require.NoError(t, l.broker.Start())
	t.Cleanup(l.broker.Close)
}

// notify raises a payload on the watch channel the way a trigger would.
func (l *listened) notify(t *testing.T, payload string) {
	t.Helper()

	require.NoError(t, testmodels.New(l.pool).NotifyWatch(t.Context(), payload))
}

// killListener ends the listener's connection from the server's side, finding it as the
// backend on this test's own database whose last statement put it on the watch channel.
func (l *listened) killListener(t *testing.T) {
	t.Helper()

	require.NoError(t, testmodels.New(l.pool).TerminateWatchListeners(t.Context()))
}

// TestListenIsOnTheChannelTheTriggersRaise pins that the connection ends up on its engine's watch channel.
func TestListenIsOnTheChannelTheTriggersRaise(t *testing.T) {
	l := newPooled(t)

	conn, err := l.pool.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()

	require.NoError(t, notifystore.NewNotifyStore(conn).OpenWatchChannel(t.Context()))

	channel, err := testmodels.New(conn).GetWatchChannel(t.Context())
	require.NoError(t, err)

	listening, err := testmodels.New(conn).ListListeningChannels(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{channel}, listening)
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
	change := waitForPassOfOneChange(t, subscription)
	require.Equal(t, "a", change.Key)
	require.NotNil(t, change.Row.GetAgent())
}

// TestALostConnectionDropsEveryWatcher pins the answer to a connection going: every
// subscription is closed, none opens until the listener is back, and the one that opens then
// receives what is raised after.
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
	require.Equal(t, "b", waitForPassOfOneChange(t, again).Key)
}

// seedAgent writes a workflow and an agent in it, and answers the agent's id.
func (l *listened) seedAgent(t *testing.T) string {
	t.Helper()

	return l.seed(t).agent
}

// seeded is the rows a lookup subresource has to find its way between.
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

// TestALookupFeederReachesItsResources pins the two lookup feeders against a real
// database: a write to a table that holds no resource key reaches the watcher under the key of
// the resource row that matches it.
func TestALookupFeederReachesItsResources(t *testing.T) {
	stores := newStores()
	l := newPooled(t)

	// The seed is written before anything listens, so only the writes below raise anything.
	rows := l.seed(t)
	l.listen(t, stores)

	subscription, err := l.broker.Subscribe(t.Context())
	require.NoError(t, err)

	stores.workflows.put(testRow{ID: rows.workflow, Name: "flow"})
	stores.agents.put(agentRow(rows.agent))

	// workflow_configs holds no workflow id: its resource is the workflow whose
	// workflow_config_id is this row's id.
	l.exec(t, `UPDATE workflow_configs SET flags = '--model opus' WHERE id = $1`, rows.config)

	change := waitForPassOfOneChange(t, subscription)
	assert.Equal(t, watchv1.Resource_RESOURCE_WORKFLOW, change.Resource)
	assert.Equal(t, rows.workflow, change.Key, "a workflow config reaches its workflow")
}

// TestALookupFeederSkipsAnArchivedResource pins that a lookup feeder with SkipParentsWith
// reaches the agent on a repository only while that agent is not archived.
func TestALookupFeederSkipsAnArchivedResource(t *testing.T) {
	l := newPooled(t)
	rows := l.seed(t)
	onRepository := Resource{
		Enum:      watchv1.Resource_RESOURCE_AGENT,
		Table:     "agents",
		KeyColumn: "id",
		Feeders: []Feeder{
			{
				Table: "pull_request_syncs",
				Lookup: &Lookup{
					ParentColumn:    "repository_url",
					ChildColumn:     "repository_url",
					SkipParentsWith: "archived_at",
				},
			},
		},
	}
	for _, trigger := range BuildTriggers([]Resource{onRepository}) {
		if trigger.Table == "pull_request_syncs" {
			l.execAsOwner(t, trigger.BuildSQL())
		}
	}
	l.exec(t, `UPDATE agents SET archived_at = now() WHERE id = $1`, rows.agent)
	conn := l.listenRaw(t)

	l.exec(t, `INSERT INTO pull_request_syncs (repository_url) VALUES ($1)`, rows.repository)
	l.exec(t, `UPDATE agents SET archived_at = NULL WHERE id = $1`, rows.agent)
	l.exec(t, `UPDATE pull_request_syncs SET updated_through = now() WHERE repository_url = $1`, rows.repository)

	assert.Equal(t, []string{
		"pull_request_syncs " + rows.repository,
		"agents " + rows.agent,
		"pull_request_syncs " + rows.repository,
		"agents " + rows.agent,
	}, waitForPayloads(t, conn, 4))
}

// TestARepositorySyncNotifiesOnlyWhatIsDrawn pins that the sync's bookkeeping writes raise
// nothing, and a completed sweep raises the repository under its URL.
func TestARepositorySyncNotifiesOnlyWhatIsDrawn(t *testing.T) {
	l := newPooled(t)
	rows := l.seed(t)
	l.exec(t, `INSERT INTO pull_request_syncs (repository_url) VALUES ($1)`, rows.repository)
	conn := l.listenRaw(t)

	l.exec(t, `UPDATE pull_request_syncs SET read_at = now(), calls = calls + 1 WHERE repository_url = $1`, rows.repository)
	l.exec(t, `UPDATE pull_request_syncs SET sweep_cursor = '', sweep_newest = now(), listed_at = now() WHERE repository_url = $1`, rows.repository)
	l.exec(t, `UPDATE agents SET name = 'renamed' WHERE id = $1`, rows.agent)
	l.exec(t, `UPDATE pull_request_syncs SET updated_through = sweep_newest, sweep_cursor = NULL, sweep_newest = NULL WHERE repository_url = $1`, rows.repository)

	assert.Equal(t, []string{"agents " + rows.agent, "pull_request_syncs " + rows.repository}, waitForPayloads(t, conn, 2))
}

func (l *listened) exec(t *testing.T, sql string, args ...any) {
	t.Helper()

	_, err := l.pool.Exec(t.Context(), sql, args...)
	require.NoError(t, err)
}

// TestAWriteReachesTheWatchersUnderItsResourceKey pins the routing the triggers do, end to end
// through a real database: a write to a resource's own table reaches a watcher under that row's
// key, and a write to one of its subresources reaches the watcher under the resource's key.
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
	require.Equal(t, agent, waitForPassOfOneChange(t, subscription).Key, "a write to the resource's own table")

	moved := agentRow(agent)
	moved.Name = "moved"
	stores.agents.put(moved)
	l.exec(t, `INSERT INTO agent_metadata (agent_id, key, value) VALUES ($1, 'branch', 'agent/x')`, agent)

	change := waitForPassOfOneChange(t, subscription)
	assert.Equal(t, agent, change.Key, "a write to a subresource reaches the watcher under its resource's key")
	assert.Equal(t, watchv1.Resource_RESOURCE_AGENT, change.Resource)
}

// TestAnUpdateOfOnlyIgnoredColumnsNotifiesNothing pins that an agent write touching only
// state_seen_at and updated_at raises nothing, and a write to any other column still does.
func TestAnUpdateOfOnlyIgnoredColumnsNotifiesNothing(t *testing.T) {
	l := newPooled(t)
	rows := l.seed(t)
	conn := l.listenRaw(t)

	l.exec(t, `UPDATE agents SET state_seen_at = now() WHERE id = $1`, rows.agent)
	l.exec(t, `UPDATE agents SET name = name WHERE id = $1`, rows.agent)
	l.exec(t, `UPDATE workflows SET name = 'other' WHERE id = $1`, rows.workflow)
	l.exec(t, `UPDATE agents SET name = 'renamed' WHERE id = $1`, rows.agent)

	assert.Equal(t, []string{"workflows " + rows.workflow, "agents " + rows.agent}, waitForPayloads(t, conn, 2))
}

// TestALookupFeederOnOneColumnNotifiesOnlyItsChange pins that a lookup feeder with OnChangeOf
// notifies its resource, under its key column, only when that column changes.
func TestALookupFeederOnOneColumnNotifiesOnlyItsChange(t *testing.T) {
	l := newPooled(t)
	rows := l.seed(t)
	byName := Resource{
		Enum:      watchv1.Resource_RESOURCE_WORKFLOW,
		Table:     "workflows",
		KeyColumn: "name",
		Feeders: []Feeder{
			{
				Table: "agents",
				Lookup: &Lookup{
					ParentColumn: "id",
					ChildColumn:  "workflow_id",
					OnChangeOf:   "archived_at",
				},
			},
		},
	}
	for _, trigger := range BuildTriggers([]Resource{byName}) {
		if trigger.Table == "agents" {
			l.execAsOwner(t, trigger.BuildSQL())
		}
	}
	conn := l.listenRaw(t)

	l.exec(t, `UPDATE agents SET name = 'renamed' WHERE id = $1`, rows.agent)
	l.exec(t, `UPDATE agents SET archived_at = now() WHERE id = $1`, rows.agent)

	assert.Equal(t, []string{"agents " + rows.agent, "agents " + rows.agent, "workflows flow"}, waitForPayloads(t, conn, 3))
}

// execAsOwner runs a statement as the database's owner.
func (l *listened) execAsOwner(t *testing.T, sql string) {
	t.Helper()

	owner, err := pgxpool.New(t.Context(), l.ownerURL)
	require.NoError(t, err)
	defer owner.Close()

	_, err = owner.Exec(t.Context(), sql)
	require.NoError(t, err)
}

// listenRaw puts a connection of its own on the watch channel.
func (l *listened) listenRaw(t *testing.T) *pgx.Conn {
	t.Helper()

	pooled, err := l.pool.Acquire(t.Context())
	require.NoError(t, err)
	t.Cleanup(pooled.Release)

	require.NoError(t, notifystore.NewNotifyStore(pooled).OpenWatchChannel(t.Context()))

	return pooled.Conn()
}

// waitForPayloads reads the next count payloads raised on the connection.
func waitForPayloads(t *testing.T, conn *pgx.Conn, count int) []string {
	t.Helper()

	var payloads []string
	for range count {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		notification, err := conn.WaitForNotification(ctx)
		cancel()
		require.NoError(t, err, "only %v were raised", payloads)
		payloads = append(payloads, notification.Payload)
	}

	return payloads
}

// TestAReviewIsRaisedByItsDoorbellAndRound pins that claiming a review's doorbell and writing
// its round raise the review, that other mail writes raise only the mail, and that nothing on
// the review raises its agent.
func TestAReviewIsRaisedByItsDoorbellAndRound(t *testing.T) {
	l := newPooled(t)
	rows := l.seed(t)
	round, doorbell, review := uuid.NewString(), uuid.NewString(), uuid.NewString()
	l.exec(t, `INSERT INTO review_rounds (id, agent_id, ordinal, base_sha, snapshot_sha) VALUES ($1, $2, 1, 'base', 'snapshot')`, round, rows.agent)
	l.exec(t, `INSERT INTO mail (id, to_agent_id, body, expires_at) VALUES ($1, $2, 'weave: review', now() + interval '1 hour')`, doorbell, rows.agent)
	l.exec(t, `INSERT INTO reviews (id, agent_id, round_id, author_kind, doorbell_mail_id) VALUES ($1, $2, $3, 'human', $4)`, review, rows.agent, round, doorbell)
	conn := l.listenRaw(t)

	l.exec(t, `UPDATE mail SET body = 'weave: review, again' WHERE id = $1`, doorbell)
	l.exec(t, `UPDATE mail SET claimed_at = now() WHERE id = $1`, doorbell)
	l.exec(t, `UPDATE review_rounds SET base_sha = 'rebased' WHERE id = $1`, round)
	l.exec(t, `UPDATE reviews SET verdict = 'approved', released_at = now() WHERE id = $1`, review)

	assert.Equal(t, []string{
		"mail " + doorbell,
		"mail " + doorbell,
		"reviews " + review,
		"reviews " + review,
		"reviews " + review,
	}, waitForPayloads(t, conn, 5))
}
