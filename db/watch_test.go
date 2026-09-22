package db_test

import (
	"testing"

	"weave/internal/dbtest"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// watched is one database with a workflow, an agent and a schedule in it, and a listener on
// the watch channel opened after they were written.
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
	w.exec(t, `INSERT INTO agents (id, name, workflow_id) VALUES ($1, 'agent', $2)`, w.agent, w.workflow)
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
// resource table raises its own key, and a projected table raises its parent's.
func TestEveryTriggeredTableRaisesItsResource(t *testing.T) {
	t.Parallel()

	w := openWatched(t)
	round := uuid.NewString()

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

// TestAStampAloneRaisesNothing pins the ignored columns: an update that moves only a column a
// loop stamps raises no notification, so the next one heard is the real write after it.
func TestAStampAloneRaisesNothing(t *testing.T) {
	t.Parallel()

	w := openWatched(t)

	w.exec(t, `UPDATE agents SET state_seen_at = now(), nudged_at = now(), nudge_sent_at = now(), updated_at = now() WHERE id = $1`, w.agent)
	w.exec(t, `UPDATE workflows SET updated_at = now() WHERE id = $1`, w.workflow)
	w.exec(t, `UPDATE workflow_configs SET updated_at = now() WHERE id = $1`, w.config)
	w.exec(t, `UPDATE schedules SET prompt = 'go' WHERE id = $1`, w.schedule)

	assert.Equal(t, "schedules "+w.schedule, w.notifications.Next(t))
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
