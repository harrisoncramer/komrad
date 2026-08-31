package agent

import (
	"context"
	"testing"
	"time"

	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArchiveAndReuse pins that reusing an archived agent's name creates a fresh row and
// that state events keyed by that name land on the live one.
func TestArchiveAndReuse(t *testing.T) {
	tests := []struct {
		name    string
		archive bool

		reuse bool

		wantVisible bool
		wantLive    int
		wantAll     int
	}{
		{
			name:        "a live agent is visible and listed",
			wantVisible: true,
			wantLive:    1,
			wantAll:     1,
		},
		{
			name:     "an archived agent is hidden but retained",
			archive:  true,
			wantLive: 0,
			wantAll:  1,
		},
		{
			name:        "reusing an archived name creates a fresh row",
			archive:     true,
			reuse:       true,
			wantVisible: true,
			wantLive:    1,
			wantAll:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%1", State: StateThinking}))
			first, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, first)

			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, "agent"))
			}
			if tt.reuse {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%2", State: StateIdle}))
			}

			got, err := st.Get(ctx, "agent")
			if !tt.wantVisible {
				require.ErrorIs(t, err, store.ErrNotFound, "an archived row is hidden from Get")
			} else {
				require.NoError(t, err)
				require.NotNil(t, got)
			}

			live, err := st.List(ctx)
			require.NoError(t, err)
			assert.Len(t, live, tt.wantLive)

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, tt.wantAll, "archived rows are retained")

			if !tt.reuse {
				return
			}

			assert.NotEqual(t, first.ID, got.ID, "a reused name must not resurrect the archived row")
			assert.Equal(t, StateIdle, got.State)
			assert.Equal(t, "%2", got.Pane)

			require.NoError(t, st.SetStateByID(ctx, got.ID, StateWaiting))

			live, err = st.List(ctx)
			require.NoError(t, err)
			require.Len(t, live, 1)
			assert.Equal(t, StateWaiting, live[0].State, "the live row takes the new state")

			all, err = st.ListAll(ctx)
			require.NoError(t, err)
			for _, agent := range all {
				if agent.ID == first.ID {
					assert.Equal(t, StateThinking, agent.State, "the archived row is left frozen")
				}
			}
		})
	}
}

// TestArchiveClearsPane guards the pane id on an archived row, which the reconciler
// never clears because it skips archived agents.
func TestArchiveClearsPane(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", Pane: "%1", State: StateThinking}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	require.NoError(t, st.archiveByName(ctx, "agent"))

	archived, err := st.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, archived)
	assert.Empty(t, archived.Pane, "an archived agent claims no pane")
}

// TestRestore pins that undeleting by id and then upserting the same id neither collides on
// the primary key nor loses what the agent belonged to.
func TestRestore(t *testing.T) {
	tests := []struct {
		name string

		withWorkflow bool

		hidden bool
	}{
		{
			name: "a restored agent keeps its id and its conversation",
		},
		{
			name:         "and the workflow it belonged to",
			withWorkflow: true,
		},
		{
			name:   "one hidden before it was archived comes back listed",
			hidden: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			workflows := st.workflows
			ctx := t.Context()

			agent := Agent{Name: "beta", State: StateThinking, SessionID: "sess-1"}
			scope := st.defaultWorkflow
			if tt.withWorkflow {
				workflow, err := workflows.Create(ctx, workflowstore.Config{Name: "backend"})
				require.NoError(t, err)
				agent.WorkflowID = workflow.ID
				scope = workflow.ID
			}

			require.NoError(t, st.Create(ctx, agent))
			original, err := st.GetInWorkflow(ctx, scope, "beta")
			require.NoError(t, err)
			require.NotNil(t, original)
			if tt.hidden {
				changed, err := st.SetHidden(ctx, original.ID, true)
				require.NoError(t, err)
				require.True(t, changed)
			}
			_, archiveErr := st.Archive(ctx, []string{original.ID})
			require.NoError(t, archiveErr)

			require.NoError(t, st.Restore(ctx, []string{original.ID}))
			restored := *original
			restored.State = StateIdle
			restored.Pane = "%9"
			require.NoError(t, st.Update(ctx, restored), "an update on restore must not collide")

			got, err := st.GetInWorkflow(ctx, scope, "beta")
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, original.ID, got.ID, "a restored row keeps its id")
			assert.Equal(t, StateIdle, got.State)
			assert.Equal(t, "sess-1", got.SessionID, "restore preserves the conversation")
			assert.False(t, got.Hidden, "a restored agent comes back to a row")
			assert.Nil(t, got.ArchivedAt, "and carries no deletion time")

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, 1, "a single row after restore")

			assert.Equal(t, original.WorkflowID, got.WorkflowID, "restore preserves the workflow")
		})
	}
}

// TestArchiveAndDeleteReturnThePaneTheRowHeld pins that an archive hands back the pane the
// row held rather than the blank it just wrote, since nothing else records the pane to kill.
func TestArchiveAndDeleteReturnThePaneTheRowHeld(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	seedPane := func(name, pane string) string {
		t.Helper()
		require.NoError(t, st.Create(ctx, Agent{Name: name, Pane: pane, State: StateIdle}))

		created, err := st.GetInWorkflow(ctx, st.defaultWorkflow, name)
		require.NoError(t, err)
		require.NotNil(t, created)

		return created.ID
	}

	archiving := seedPane("archiving", "%4")
	deleting := seedPane("deleting", "%5")

	archived, err := st.Archive(ctx, []string{archiving})
	require.NoError(t, err)
	assert.Equal(t, []AgentPane{{AgentID: archiving, Pane: "%4"}}, archived)

	deleted, err := st.Delete(ctx, []string{deleting})
	require.NoError(t, err)
	assert.Equal(t, []AgentPane{{AgentID: deleting, Pane: "%5"}}, deleted)
}

// TestArchiveReturnsThePaneAWriteItWaitedOnLeftBehind pins that the archive hands back a pane
// recorded by a write it was blocked on, which a plain join misses because only the row being
// updated is re-read once the lock is released.
func TestArchiveReturnsThePaneAWriteItWaitedOnLeftBehind(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "dormant", State: StateIdle}))

	created, err := st.GetInWorkflow(ctx, st.defaultWorkflow, "dormant")
	require.NoError(t, err)
	require.NotNil(t, created)

	tx, err := st.pool.GetRawPool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `UPDATE agents SET pane_id = '%9' WHERE id = $1`, created.ID)
	require.NoError(t, err)

	type outcome struct {
		taken []AgentPane
		err   error
	}

	archived := make(chan outcome, 1)
	go func() {
		taken, archiveErr := st.Archive(context.WithoutCancel(ctx), []string{created.ID})
		archived <- outcome{taken: taken, err: archiveErr}
	}()

	waitForBlockedArchive(t, st)
	require.NoError(t, tx.Commit(ctx))

	got := <-archived
	require.NoError(t, got.err)
	assert.Equal(t, []AgentPane{{AgentID: created.ID, Pane: "%9"}}, got.taken)
}

// waitForBlockedArchive blocks until an archive is waiting on a row lock, which is how the test
// knows the archive has reached its write rather than not having started yet.
func waitForBlockedArchive(t *testing.T, st *fixture) {
	t.Helper()

	for range 500 {
		var waiting bool
		require.NoError(t, st.pool.GetRawPool().QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT
					1
				FROM
					pg_stat_activity
				WHERE
					wait_event_type = 'Lock'
					AND query LIKE '%MarkAgentsArchived%')`).Scan(&waiting))

		if waiting {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("no archive ever blocked on the row, so it never reached its write")
}

// TestArchiveAndRestoreInBatches pins that a write keyed on a list of ids never acts on a
// row nobody named.
func TestArchiveAndRestoreInBatches(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	other, err := st.workflows.Create(ctx, workflowstore.Config{Name: "frontend"})
	require.NoError(t, err)

	names := map[string]string{"auth": st.defaultWorkflow, "api": st.defaultWorkflow, "ui": other.ID}
	ids := make(map[string]string, len(names))
	for name, workflowID := range names {
		require.NoError(t, st.Create(ctx, Agent{Name: name, Pane: "%1", State: StateIdle, WorkflowID: workflowID}))

		created, err := st.GetInWorkflow(ctx, workflowID, name)
		require.NoError(t, err)
		require.NotNil(t, created)
		ids[name] = created.ID
	}

	_, archiveErr := st.Archive(ctx, []string{ids["auth"], ids["ui"]})

	require.NoError(t, archiveErr)

	live, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1, "the agent nobody named is untouched")
	assert.Equal(t, "api", live[0].Name)

	for _, name := range []string{"auth", "ui"} {
		archived, err := st.GetByID(ctx, ids[name])
		require.NoError(t, err)
		require.NotNil(t, archived)
		assert.NotNil(t, archived.ArchivedAt, "%s is archived", name)
		assert.Empty(t, archived.Pane, "and claims no pane")
	}

	require.NoError(t, st.Restore(ctx, []string{ids["auth"], ids["ui"]}))

	live, err = st.List(ctx)
	require.NoError(t, err)
	assert.Len(t, live, 3, "the batch came back whole, across both workflows")
}
