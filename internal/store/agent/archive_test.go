package agent

import (
	"context"
	"testing"
	"time"

	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArchiveAndReuse pins that reusing an archived agent's name creates a fresh row and
// that state events keyed by that name land on the live one.
func TestArchiveAndReuse(t *testing.T) {
	tests := []struct {
		name        string
		archive     bool
		reuse       bool
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

			require.NoError(t, st.Create(ctx, Agent{
				Name:  "agent",
				Pane:  "%1",
				State: StateThinking,
			}))
			first, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, first)

			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, "agent"))
			}
			if tt.reuse {
				require.NoError(t, st.Create(ctx, Agent{
					Name:  "agent",
					Pane:  "%2",
					State: StateIdle,
				}))
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
					assert.Equal(t, StateIdle, agent.State, "the archived row is settled, not written to")
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

	require.NoError(t, st.Create(ctx, Agent{
		Name:  "agent",
		Pane:  "%1",
		State: StateThinking,
	}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	require.NoError(t, st.archiveByName(ctx, "agent"))

	archived, err := st.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, archived)
	assert.Empty(t, archived.Pane, "an archived agent claims no pane")
}

// TestArchiveSettlesTheState covers each archive write leaving the row as the reconciler would
// have once it saw the pane gone, with a setup the archive cut short settled the same way and
// a failure kept.
func TestArchiveSettlesTheState(t *testing.T) {
	writes := map[string]func(ctx context.Context, st *fixture, agent Agent) error{
		"by id": func(ctx context.Context, st *fixture, agent Agent) error {
			_, err := st.Archive(ctx, []string{agent.ID})

			return err
		},
		"by workflow": func(ctx context.Context, st *fixture, agent Agent) error {
			_, err := st.ArchiveWorkflowAgents(ctx, agent.WorkflowID)

			return err
		},
		"if unchanged": func(ctx context.Context, st *fixture, agent Agent) error {
			_, err := st.ArchiveIfUnchanged(ctx, ArchiveIfUnchangedParams{ID: agent.ID, UpdatedAt: agent.UpdatedAt})

			return err
		},
	}

	tests := []struct {
		name            string
		state           State
		wantState       State
		wantInterrupted bool
		wantReason      string
	}{
		{
			name:            "thinking is a turn cut part-way",
			state:           StateThinking,
			wantState:       StateIdle,
			wantInterrupted: true,
			wantReason:      "",
		},
		{
			name:            "waiting is a turn cut part-way",
			state:           StateWaiting,
			wantState:       StateIdle,
			wantInterrupted: true,
			wantReason:      "",
		},
		{
			name:            "uncertain settles with no turn to ask for again",
			state:           StateUncertain,
			wantState:       StateIdle,
			wantInterrupted: false,
			wantReason:      "",
		},
		{
			name:            "setting up settles with no turn to ask for again",
			state:           StateSettingUp,
			wantState:       StateIdle,
			wantInterrupted: false,
			wantReason:      "",
		},
		{
			name:            "idle stays as it was",
			state:           StateIdle,
			wantState:       StateIdle,
			wantInterrupted: false,
			wantReason:      "reading",
		},
		{
			name:            "a failed create keeps its failure",
			state:           StateFailedToCreate,
			wantState:       StateFailedToCreate,
			wantInterrupted: false,
			wantReason:      "reading",
		},
	}

	for via, archive := range writes {
		t.Run(via, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					st := open(t)
					ctx := t.Context()

					require.NoError(t, st.Create(ctx, Agent{
						Name:  "agent",
						Pane:  "%1",
						State: tt.state,
					}))
					created, err := st.Get(ctx, "agent")
					require.NoError(t, err)
					require.NotNil(t, created)
					require.NoError(t, st.SetReportedReason(ctx, created.ID, "reading"))

					before, err := st.GetByID(ctx, created.ID)
					require.NoError(t, err)
					require.NotNil(t, before)

					require.NoError(t, archive(ctx, st, *before))

					archived, err := st.GetByID(ctx, created.ID)
					require.NoError(t, err)
					require.NotNil(t, archived)
					require.NotNil(t, archived.ArchivedAt)

					assert.Equal(t, tt.wantState, archived.State)
					assert.Equal(t, tt.wantReason, archived.ReportedReason)
					assert.Equal(t, tt.wantInterrupted, archived.InterruptedAt != nil)
					if tt.wantState != tt.state {
						assert.True(t, archived.StateChangedAt.After(before.StateChangedAt), "a settled state moves the change stamp")
						assert.True(t, archived.StateSeenAt.After(before.StateSeenAt), "a settled state is a fresh reading")
					} else {
						assert.Equal(t, before.StateChangedAt, archived.StateChangedAt, "a state left alone keeps its change stamp")
						assert.Equal(t, before.StateSeenAt, archived.StateSeenAt, "a state left alone keeps its reading")
					}
				})
			}
		})
	}
}

// TestRestore pins that undeleting by id and then upserting the same id neither collides on
// the primary key nor loses what the agent belonged to.
func TestRestore(t *testing.T) {
	tests := []struct {
		name         string
		withWorkflow bool
		hidden       bool
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

			agent := Agent{
				Name:      "beta",
				State:     StateThinking,
				SessionID: "sess-1",
			}
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

// TestArchiveAndDeleteReturnThePaneTheRowHeld pins that an archive hands back the pane the row
// held rather than the blank it just wrote.
func TestArchiveAndDeleteReturnThePaneTheRowHeld(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	seedPane := func(name, pane string) string {
		t.Helper()
		require.NoError(t, st.Create(ctx, Agent{
			Name:  name,
			Pane:  pane,
			State: StateIdle,
		}))

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

// waitForBlockedArchive blocks until an archive is waiting on a row lock.
func waitForBlockedArchive(t *testing.T, st *fixture) {
	t.Helper()

	for range 500 {
		var waiting bool
		require.NoError(t, st.pool.GetRawPool().QueryRow(t.Context(), `
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
		require.NoError(t, st.Create(ctx, Agent{
			Name:       name,
			Pane:       "%1",
			State:      StateIdle,
			WorkflowID: workflowID,
		}))

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

// TestListForTranscriptSync pins which agents the transcript loop is handed, an agent with no
// conversation to read being the one that would otherwise never leave it.
func TestListForTranscriptSync(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		archive   bool
		synced    bool
		wantsRead bool
	}{
		{
			name:      "a live agent is always read",
			sessionID: "claude-live",
			wantsRead: true,
		},
		{
			name:      "a live agent with no session yet is still read",
			wantsRead: true,
		},
		{
			name:      "an agent archived before its last sync is read once more",
			sessionID: "claude-tail",
			archive:   true,
			wantsRead: true,
		},
		{
			name:      "an agent synced since it was archived is done with",
			sessionID: "claude-settled",
			archive:   true,
			synced:    true,
		},
		{
			name:    "an archived agent that never held a conversation is done with",
			archive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t)
			ctx := t.Context()

			require.NoError(t, f.Create(ctx, Agent{
				Name:      "subject",
				State:     StateIdle,
				SessionID: tt.sessionID,
			}))
			subject, err := f.Get(ctx, "subject")
			require.NoError(t, err)

			if tt.archive {
				require.NoError(t, f.archiveByName(ctx, "subject"))
			}
			if tt.synced {
				seedSyncedSession(t, f, subject.ID, tt.sessionID)
			}

			listed, err := f.ListForTranscriptSync(ctx)
			require.NoError(t, err)

			ids := xslice.Map(listed, func(agent Agent) string {
				return agent.ID
			})

			if tt.wantsRead {
				assert.Contains(t, ids, subject.ID)

				return
			}
			assert.NotContains(t, ids, subject.ID)
		})
	}
}

// seedSyncedSession records a conversation read after the agent holding it was archived.
func seedSyncedSession(t *testing.T, f *fixture, agentID, sessionID string) {
	t.Helper()
	id, err := store.NewID()
	require.NoError(t, err)

	_, err = f.db.Exec(t.Context(),
		`INSERT INTO agent_transcript_sessions (id, agent_id, session_id, synced_at) VALUES ($1, $2, $3, NOW())`,
		id, agentID, sessionID)
	require.NoError(t, err)
}
