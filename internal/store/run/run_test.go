package run

import (
	"testing"

	"weave/internal/pgtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	*Store
	db     store.DBTX
	agents *agentstore.Store
	// workflow is the one every seeded agent is filed under, since a run hangs off an agent
	// and an agent hangs off a workflow.
	workflow string
}

func open(t *testing.T) *fixture {
	t.Helper()

	pool, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	workflows := workflowstore.New(pool.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{
		Store:    New(pool.GetDB()),
		db:       pool.GetDB(),
		agents:   agentstore.New(pool.GetDB()),
		workflow: created.ID,
	}
}

func (f *fixture) seedAgent(t *testing.T) string {
	t.Helper()

	id := uuid.NewString()
	require.NoError(t, f.agents.Create(t.Context(), agentstore.Agent{
		ID:         id,
		Name:       "agent-" + id,
		Cwd:        "/repo",
		State:      agentstore.StateIdle,
		WorkflowID: f.workflow,
	}))

	return id
}

// TestBeginRefusesAnIdNoAgentHolds pins that a run is refused rather than recorded against
// nothing, since the id reaches the store from a caller that may have made it up.
func TestBeginRefusesAnIdNoAgentHolds(t *testing.T) {
	f := open(t)

	_, err := f.Begin(t.Context(), BeginParams{
		AgentID: "not-an-id",
		Trigger: "post-launch",
		Script:  "setup",
	})

	require.Error(t, err)
}

// TestFinishRecordsWhatTheRunDid covers the two halves of a run: the row opened before it
// starts, and the failure written onto it after, which is what the panel's mark reads.
func TestFinishRecordsWhatTheRunDid(t *testing.T) {
	tests := []struct {
		name       string
		exitCode   *int32
		failure    string
		wantUnread bool
	}{
		{
			name:     "a run that succeeded is nothing to read",
			exitCode: xptr.Ptr(int32(0)),
		},
		{
			name:       "a run that exited badly is held for the person",
			exitCode:   xptr.Ptr(int32(3)),
			failure:    "a hook failed: setup: exited 3",
			wantUnread: true,
		},
		{
			name:       "a run refused before it started has no status of its own",
			failure:    "a hook failed: setup: unusable working directory",
			wantUnread: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t)
			agentID := f.seedAgent(t)

			id, err := f.Begin(t.Context(), BeginParams{
				AgentID:   agentID,
				Trigger:   "post-launch",
				Script:    "setup",
				LogNumber: xptr.Ptr(int32(7)),
			})
			require.NoError(t, err)

			unread, err := f.ListUnreadFailures(t.Context())
			require.NoError(t, err)
			assert.Empty(t, unread, "a run still going is not a failure yet")

			require.NoError(t, f.Finish(t.Context(), FinishParams{
				ID:       id,
				ExitCode: tt.exitCode,
				Failure:  tt.failure,
			}))

			unread, err = f.ListUnreadFailures(t.Context())
			require.NoError(t, err)
			if !tt.wantUnread {
				assert.Empty(t, unread)

				return
			}

			assert.Equal(t, []string{agentID}, unread)

			require.NoError(t, f.MarkRead(t.Context(), agentID))

			unread, err = f.ListUnreadFailures(t.Context())
			require.NoError(t, err)
			assert.Empty(t, unread, "reading it clears the mark")
		})
	}
}

// TestAbandonDropsTheRow covers the refusal that is not a run at all, where the log file is
// removed too and a row left behind would spend one of the fifty an agent keeps.
func TestAbandonDropsTheRow(t *testing.T) {
	f := open(t)
	agentID := f.seedAgent(t)

	id, err := f.Begin(t.Context(), BeginParams{
		AgentID: agentID,
		Trigger: "open-pr",
		Script:  "_open_pr",
	})
	require.NoError(t, err)
	require.NoError(t, f.Finish(t.Context(), FinishParams{ID: id, Failure: "it would not start"}))

	require.NoError(t, f.Abandon(t.Context(), id))

	unread, err := f.ListUnreadFailures(t.Context())
	require.NoError(t, err)
	assert.Empty(t, unread)
}

// TestPruneKeepsTheNewest holds the rows to the same bound the log files are held to, so a row
// is never left pointing at output the sweep has taken.
func TestPruneKeepsTheNewest(t *testing.T) {
	f := open(t)
	agentID := f.seedAgent(t)
	other := f.seedAgent(t)

	var ids []string
	for range 5 {
		id, err := f.Begin(t.Context(), BeginParams{
			AgentID: agentID,
			Trigger: "resume",
			Script:  "worktree",
		})
		require.NoError(t, err)
		require.NoError(t, f.Finish(t.Context(), FinishParams{ID: id, Failure: "it failed"}))
		ids = append(ids, id)
	}

	kept, err := f.Begin(t.Context(), BeginParams{
		AgentID: other,
		Trigger: "resume",
		Script:  "worktree",
	})
	require.NoError(t, err)
	require.NoError(t, f.Finish(t.Context(), FinishParams{ID: kept, Failure: "it failed"}))

	require.NoError(t, f.Prune(t.Context(), agentID, 2))

	remaining := f.runIDs(t, agentID)
	assert.Equal(t, ids[3:], remaining, "the oldest three went and the newest two stayed")
	assert.Equal(t, []string{kept}, f.runIDs(t, other), "another agent's runs are left alone")
}

// runIDs reads an agent's rows oldest first, which nothing in the store does because nothing
// above it reads a run back yet.
func (f *fixture) runIDs(t *testing.T, agentID string) []string {
	t.Helper()

	rows, err := f.db.Query(t.Context(), "SELECT id FROM agent_runs WHERE agent_id = $1 ORDER BY started_at, id", agentID)
	require.NoError(t, err)
	defer rows.Close()

	found := make([]string, 0, 8)
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		found = append(found, id)
	}
	require.NoError(t, rows.Err())

	return found
}
