package db_test

import (
	"testing"

	"weave/internal/dbtest"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// watchedRepository is the repository the seeded agent is checked out of.
const watchedRepository = "https://github.com/o/r"

// watched is one database with a workflow, an agent on a branch and a schedule in it, and a
// listener on the watch channel opened after they were written.
type watched struct {
	conn          *pgx.Conn
	notifications *dbtest.Notifications
	workflow      string
	config        string
	provisioner   string
	agent         string
	schedule      string
}

func openWatched(t *testing.T) *watched {
	t.Helper()

	ctx := t.Context()
	url := dbtest.URL(t)

	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(t.Context()) })

	w := &watched{
		conn:        conn,
		workflow:    uuid.NewString(),
		config:      uuid.NewString(),
		provisioner: uuid.NewString(),
		agent:       uuid.NewString(),
		schedule:    uuid.NewString(),
	}
	w.exec(t, `INSERT INTO workflow_configs (id, harness, cwd, flags) VALUES ($1, 'claude', '/repo', '')`, w.config)
	w.exec(t, `INSERT INTO workflow_provisioner_configs (id, worktree_enabled, worktree_base_branch, worktree_dir, worktree_copy_paths, worktree_symlink_paths, worktree_diff_exclude)
		VALUES ($1, false, 'main', '', '{}', '{}', '{}')`, w.provisioner)
	w.exec(t, `INSERT INTO workflows (id, name, context, workflow_config_id, provisioner_config_id) VALUES ($1, 'flow', '', $2, $3)`, w.workflow, w.config, w.provisioner)
	w.exec(t, `INSERT INTO agents (id, name, workflow_id, repository_url, branch) VALUES ($1, 'agent', $2, $3, 'agent/x')`, w.agent, w.workflow, watchedRepository)
	w.exec(t, `INSERT INTO schedules (id, name, cron, workflow_id) VALUES ($1, 'nightly', '0 0 * * *', $2)`, w.schedule, w.workflow)

	w.notifications = dbtest.Listen(t, url)

	return w
}

func (w *watched) exec(t *testing.T, sql string, args ...any) {
	t.Helper()

	_, err := w.conn.Exec(t.Context(), sql, args...)
	require.NoError(t, err)
}

// TestEveryTriggeredTableRaisesItsResource pins the trigger on each table the watch reads: a
// resource table raises its own key, a projected table raises its parent's, and a table that is
// a resource of its own raises nothing for the row it is drawn beside.
func TestEveryTriggeredTableRaisesItsResource(t *testing.T) {
	t.Parallel()

	w := openWatched(t)
	round := uuid.NewString()
	mail := uuid.NewString()
	pullRequest := uuid.NewString()

	tests := []struct {
		name  string
		write string
		args  []any
		want  string
	}{
		{
			name:  "an agent update",
			write: `UPDATE agents SET name = 'renamed' WHERE id = $1`,
			args:  []any{w.agent},
			want:  "agents " + w.agent,
		},
		{
			name:  "a workflow update",
			write: `UPDATE workflows SET context = 'read the docs' WHERE id = $1`,
			args:  []any{w.workflow},
			want:  "workflows " + w.workflow,
		},
		{
			name:  "a schedule update",
			write: `UPDATE schedules SET enabled = false WHERE id = $1`,
			args:  []any{w.schedule},
			want:  "schedules " + w.schedule,
		},
		{
			name:  "a metadata insert",
			write: `INSERT INTO agent_metadata (agent_id, key, value) VALUES ($1, 'ticket', 'W-1')`,
			args:  []any{w.agent},
			want:  "agents " + w.agent,
		},
		{
			name:  "a run insert",
			write: `INSERT INTO agent_runs (agent_id, trigger, script, failure) VALUES ($1, 'pre-launch', 'setup', '')`,
			args:  []any{w.agent},
			want:  "agents " + w.agent,
		},
		{
			name:  "a round insert",
			write: `INSERT INTO review_rounds (id, agent_id, ordinal, base_sha, snapshot_sha) VALUES ($1, $2, 1, 'base', 'snapshot')`,
			args:  []any{round, w.agent},
			want:  "agents " + w.agent,
		},
		{
			name:  "a review insert",
			write: `INSERT INTO reviews (agent_id, round_id, author_kind, body, path) VALUES ($1, $2, 'human', '', '')`,
			args:  []any{w.agent, round},
			want:  "agents " + w.agent,
		},
		{
			name:  "a hook insert",
			write: `INSERT INTO workflow_hooks (workflow_id, event, script, position) VALUES ($1, 'pre-launch', 'setup', 0)`,
			args:  []any{w.workflow},
			want:  "workflows " + w.workflow,
		},
		{
			name:  "a config update",
			write: `UPDATE workflow_configs SET flags = '--verbose' WHERE id = $1`,
			args:  []any{w.config},
			want:  "workflows " + w.workflow,
		},
		{
			name:  "a provisioner config update",
			write: `UPDATE workflow_provisioner_configs SET worktree_enabled = true WHERE id = $1`,
			args:  []any{w.provisioner},
			want:  "workflows " + w.workflow,
		},
		{
			name:  "a mark on an agent",
			write: `INSERT INTO marks (mark, agent_id) VALUES ('a', $1)`,
			args:  []any{w.agent},
			want:  "marks a",
		},
		{
			name:  "a mark on a workflow",
			write: `INSERT INTO marks (mark, workflow_id) VALUES ('b', $1)`,
			args:  []any{w.workflow},
			want:  "marks b",
		},
		{
			name:  "a mark cleared",
			write: `DELETE FROM marks WHERE mark = 'a'`,
			want:  "marks a",
		},
		{
			name:  "mail sent",
			write: `INSERT INTO mail (id, from_name, to_agent_id, body, kind, expires_at) VALUES ($1, 'weave', $2, 'hello', 'plain', now() + interval '1 hour')`,
			args:  []any{mail, w.agent},
			want:  "mail " + mail,
		},
		{
			name:  "mail claimed",
			write: `UPDATE mail SET claimed_at = now() WHERE id = $1`,
			args:  []any{mail},
			want:  "mail " + mail,
		},
		{
			name: "a pull request on a live agent's branch",
			write: `INSERT INTO pull_requests (id, host, forge, provider_id, url, repository_url, head_repository_url, number, title, head_branch, head_sha, base_branch, base_sha, draft, state, opened_at, changed_at)
				VALUES ($1, 'github.com', 'github', 'PR_1', 'https://github.com/o/r/pull/1', $2, $2, 1, 'one', 'agent/x', 'abc', 'main', 'def', false, 'OPEN', now(), now())`,
			args: []any{pullRequest, watchedRepository},
			want: "pull_requests " + pullRequest,
		},
		{
			name:  "a repository sync moving its high-water mark",
			write: `INSERT INTO pull_request_syncs (repository_url, updated_through) VALUES ($1, now())`,
			args:  []any{watchedRepository},
			want:  "agents " + w.agent,
		},
		{
			name:  "a delete",
			write: `DELETE FROM schedules WHERE id = $1`,
			args:  []any{w.schedule},
			want:  "schedules " + w.schedule,
		},
	}

	for _, tt := range tests {
		w.exec(t, tt.write, tt.args...)
		assert.Equal(t, tt.want, w.notifications.Next(t), tt.name)
	}
}

// TestAProjectedRowWithNoParentRaisesNothing pins a config row written before the workflow that
// points at it, which has no parent to name.
func TestAProjectedRowWithNoParentRaisesNothing(t *testing.T) {
	t.Parallel()

	w := openWatched(t)

	w.exec(t, `INSERT INTO workflow_configs (id, harness, cwd, flags) VALUES ($1, 'claude', '/repo', '')`, uuid.NewString())
	w.exec(t, `UPDATE schedules SET prompt = 'go' WHERE id = $1`, w.schedule)

	assert.Equal(t, "schedules "+w.schedule, w.notifications.Next(t))
}

// TestAKeyThatChangesRaisesBoth pins an update to a row's key naming the key it had and the one
// it has, so the old one tombstones and the new one lands.
func TestAKeyThatChangesRaisesBoth(t *testing.T) {
	t.Parallel()

	w := openWatched(t)
	renamed := uuid.NewString()

	w.exec(t, `UPDATE schedules SET id = $2 WHERE id = $1`, w.schedule, renamed)

	assert.Equal(t, "schedules "+w.schedule, w.notifications.Next(t))
	assert.Equal(t, "schedules "+renamed, w.notifications.Next(t))
}

// TestOneTransactionRaisesOnceForOneRow pins Postgres folding the identical notifications a
// transaction raises, so five metadata rows for one agent are one notification.
func TestOneTransactionRaisesOnceForOneRow(t *testing.T) {
	t.Parallel()

	w := openWatched(t)

	tx, err := w.conn.Begin(t.Context())
	require.NoError(t, err)
	for _, key := range []string{"a", "b", "c"} {
		_, err := tx.Exec(t.Context(), `INSERT INTO agent_metadata (agent_id, key, value) VALUES ($1, $2, '')`, w.agent, key)
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(t.Context()))
	w.exec(t, `UPDATE schedules SET prompt = 'go' WHERE id = $1`, w.schedule)

	assert.Equal(t, "agents "+w.agent, w.notifications.Next(t))
	assert.Equal(t, "schedules "+w.schedule, w.notifications.Next(t))
}

// TestAStampAloneRaisesNothing pins the ignored columns: an update that moves only a column a
// loop stamps raises no notification, so the next ones heard are the real writes after them,
// a mail's report stamp being the one write at expiry that does move its row.
func TestAStampAloneRaisesNothing(t *testing.T) {
	t.Parallel()

	w := openWatched(t)
	mail := uuid.NewString()
	pullRequest := uuid.NewString()
	w.exec(t, `INSERT INTO mail (id, from_name, to_agent_id, body, kind, expires_at) VALUES ($1, 'weave', $2, 'hello', 'plain', now())`, mail, w.agent)
	w.exec(t, `INSERT INTO pull_requests (id, host, forge, provider_id, url, repository_url, head_repository_url, number, title, head_branch, head_sha, base_branch, base_sha, draft, state, opened_at, changed_at)
		VALUES ($1, 'github.com', 'github', 'PR_1', 'https://github.com/o/r/pull/1', $2, $2, 1, 'one', 'agent/x', 'abc', 'main', 'def', false, 'OPEN', now(), now())`, pullRequest, watchedRepository)
	w.exec(t, `INSERT INTO pull_request_syncs (repository_url) VALUES ($1)`, watchedRepository)
	for range 3 {
		w.notifications.Next(t)
	}

	w.exec(t, `UPDATE agents SET state_seen_at = now(), nudged_at = now(), nudge_sent_at = now(), updated_at = now() WHERE id = $1`, w.agent)
	w.exec(t, `UPDATE workflows SET updated_at = now() WHERE id = $1`, w.workflow)
	w.exec(t, `UPDATE workflow_configs SET updated_at = now() WHERE id = $1`, w.config)
	w.exec(t, `UPDATE mail SET updated_at = now() WHERE id = $1`, mail)
	w.exec(t, `UPDATE pull_requests SET checks_read_at = now() WHERE id = $1`, pullRequest)
	w.exec(t, `UPDATE pull_request_syncs SET read_at = now(), calls = calls + 1, listed_at = now(), sweep_cursor = '', sweep_newest = now() WHERE repository_url = $1`, watchedRepository)

	w.exec(t, `UPDATE mail SET reported_at = now() WHERE id = $1`, mail)
	w.exec(t, `UPDATE schedules SET prompt = 'go' WHERE id = $1`, w.schedule)

	assert.Equal(t, "mail "+mail, w.notifications.Next(t))
	assert.Equal(t, "schedules "+w.schedule, w.notifications.Next(t))
}
