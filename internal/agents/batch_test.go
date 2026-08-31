package agents

import (
	"testing"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArchiveAgentsTakesTheWholeBatch covers archiving several agents at once, which need not
// share a workflow and whose panes go with them.
func TestArchiveAgentsTakesTheWholeBatch(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	backend, frontend := seedWorkflow(t, st), seedWorkflow(t, st)
	auth := seedRunningIn(t, st, tmux, backend, "auth", "%1")
	api := seedRunningIn(t, st, tmux, backend, "api", "%2")

	ui := seedInWorkflow(t, st, frontend, "ui", "", agentstore.StateIdle)

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{auth.ID, api.ID, ui.ID}))

	for _, agent := range []agentstore.Agent{auth, api, ui} {
		assert.NotNil(t, agentByID(t, st, agent.ID).ArchivedAt, "%s is archived", agent.Name)
	}

	assert.ElementsMatch(t, []string{"%1", "%2"}, tmux.killedPanes, "only the running agents had a pane to kill")

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, live, "the batch spanned both workflows")
}

// TestArchiveUnchangedAgents covers the guard a background pass needs, since one deciding from
// an older read would hide an agent that has just come back.
func TestArchiveUnchangedAgents(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	settled := seedRunningIn(t, st, tmux, workflowID, "settled", "%1")
	moved := seedInWorkflow(t, st, workflowID, "moved", "", agentstore.StateIdle)

	observed := []agentstore.Agent{settled, moved}

	require.NoError(t, st.Agents.SetStateByID(ctx, moved.ID, agentstore.StateThinking))

	archived, err := manager.lifecycle.ArchiveUnchangedAgents(ctx, observed)
	require.NoError(t, err)

	require.Len(t, archived, 1)
	assert.Equal(t, settled.ID, archived[0].ID, "only the row that still read as it was observed is archived")
	assert.NotNil(t, agentByID(t, st, settled.ID).ArchivedAt)
	assert.Nil(t, agentByID(t, st, moved.ID).ArchivedAt, "the row that moved since the snapshot is left for a later pass")
	assert.Equal(t, []string{"%1"}, tmux.killedPanes, "the pane goes with the archive it belongs to")
}

// TestArchiveAgentsRefusesTheWholeBatch pins one bad id refusing the batch, since nothing
// would undo the half that landed.
func TestArchiveAgentsRefusesTheWholeBatch(t *testing.T) {
	tests := []struct {
		name string

		stray string
	}{
		{
			name:  "an id nobody holds",
			stray: uuid.NewString(),
		},
		{
			name:  "a value that is not an id at all",
			stray: "not-an-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			auth := seedRunning(t, st, tmux, "auth", "%1", agentstore.StateThinking)
			api := seedRunning(t, st, tmux, "api", "%2", agentstore.StateThinking)

			err := manager.lifecycle.ArchiveAgents(ctx, []string{auth.ID, tt.stray, api.ID})

			require.ErrorIs(t, err, ErrAgentNotFound)
			require.ErrorContains(t, err, tt.stray, "the refusal names what it could not find")
			assert.Empty(t, tmux.killedPanes, "nothing was stopped")

			live, err := st.List(ctx)
			require.NoError(t, err)
			assert.Len(t, live, 2, "and nothing was archived")
		})
	}
}

// TestDeleteAgentsFiresEveryWorkflowsCleanup pins each agent's own cleanup firing, since the
// action belongs to the workflow rather than to the batch.
func TestDeleteAgentsFiresEveryWorkflowsCleanup(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)
	workflows := workflowsFor(st, tmux)
	ctx := t.Context()

	backend, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend", CleanupAction: "drop-worktree"})
	require.NoError(t, err)
	frontend, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "frontend", CleanupAction: "drop-container"})
	require.NoError(t, err)

	auth, err := manager.launch.NewAgent(ctx, NewAgentParams{Title: "auth", Dir: t.TempDir(), WorkflowID: backend.ID})
	require.NoError(t, err)
	ui, err := manager.launch.NewAgent(ctx, NewAgentParams{Title: "ui", Dir: t.TempDir(), WorkflowID: frontend.ID})
	require.NoError(t, err)

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{auth.ID, ui.ID}))

	assert.ElementsMatch(t, []string{"drop-worktree", "drop-container"}, lifecycle.cleanup,
		"which agent goes first is the listing's order rather than the caller's")

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
}

// TestDeleteAgentsTakesTheirTranscripts pins the conversations going with the rows, since one
// left behind would keep answering the SQL tools.
func TestDeleteAgentsTakesTheirTranscripts(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	auth := seedRunning(t, st, tmux, "auth", "%1", agentstore.StateThinking)
	api := seed(t, st, "api", "%2", agentstore.StateIdle)

	for _, agent := range []agentstore.Agent{auth, api} {
		require.NoError(t, st.Transcripts.Upsert(ctx, transcriptstore.UpsertParams{
			AgentID: agent.ID, Content: "something " + agent.Name + " said", Offset: 1,
		}))
	}

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{auth.ID, api.ID}))

	for _, agent := range []agentstore.Agent{auth, api} {
		stored, err := st.GetTranscript(ctx, agent.ID)
		require.NoError(t, err)
		assert.Empty(t, stored.Content, "%s left no conversation behind", agent.Name)
	}
}

// TestDeleteAgentsRefusesTheWholeBatch pins that no cleanup fires, which would have torn down
// a directory for an agent still in the panel.
func TestDeleteAgentsRefusesTheWholeBatch(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)
	ctx := t.Context()

	created, err := workflowsFor(st, tmux).NewWorkflow(ctx, workflowstore.Config{Name: "backend", CleanupAction: "drop-worktree"})
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(ctx, NewAgentParams{Title: "auth", Dir: t.TempDir(), WorkflowID: created.ID})
	require.NoError(t, err)

	err = manager.lifecycle.DeleteAgents(ctx, []string{agent.ID, uuid.NewString()})

	require.ErrorIs(t, err, ErrAgentNotFound)
	assert.Empty(t, lifecycle.cleanup, "no workspace was torn down")
	assert.Empty(t, tmux.killedPanes, "and nothing was stopped")

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

// TestRestoreAgentsBringsBackEveryWorkflow pins each workflow coming back with its agents,
// since one restored under a workflow nothing lists would get no row anywhere.
func TestRestoreAgentsBringsBackEveryWorkflow(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	workflows := workflowsFor(st, tmux)
	ctx := t.Context()

	backend, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
	require.NoError(t, err)
	frontend, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "frontend"})
	require.NoError(t, err)

	auth := seedInWorkflow(t, st, backend.ID, "auth", "", agentstore.StateIdle)
	ui := seedInWorkflow(t, st, frontend.ID, "ui", "", agentstore.StateIdle)

	require.NoError(t, workflows.ArchiveWorkflow(ctx, backend.ID))
	require.NoError(t, workflows.ArchiveWorkflow(ctx, frontend.ID))

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{auth.ID, ui.ID})
	require.NoError(t, err)
	require.Len(t, restored, 2)

	listed, err := st.Workflows.List(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"backend", "frontend"}, workflowNames(listed), "both folders came back")

	for _, agent := range restored {
		assert.Nil(t, agent.ArchivedAt, "%s is back in the listing", agent.Name)
	}
}

// TestRestoreAgentsRefusesTheWholeBatch pins one bad id refusing the batch, so a stale archive
// listing does not half-restore a folder.
func TestRestoreAgentsRefusesTheWholeBatch(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	ctx := t.Context()

	archived := seed(t, st, "auth", "", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "auth"))

	_, err := manager.lifecycle.RestoreAgents(ctx, []string{archived.ID, uuid.NewString()})

	require.ErrorIs(t, err, ErrAgentNotFound)
	assert.NotNil(t, agentByID(t, st, archived.ID).ArchivedAt, "the agent stays in the archive")
}

// TestRestoreAgentsCountsTheWholeBatchAgainstTheCeiling pins the whole batch being counted,
// since asking one at a time would let five through a panel with room for two.
func TestRestoreAgentsCountsTheWholeBatchAgainstTheCeiling(t *testing.T) {
	st := newStore(t)
	manager := st.agentServiceCapped(newFakeTmux(), &fakeLifecycle{}, 3)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	archived := make([]string, 0, 3)
	for _, name := range []string{"auth", "api", "db"} {
		agent := seedInWorkflow(t, st, workflowID, name, "", agentstore.StateIdle)
		archived = append(archived, agent.ID)
	}
	for _, name := range []string{"auth", "api", "db"} {
		require.NoError(t, st.SoftDelete(ctx, name))
	}

	seedInWorkflow(t, st, workflowID, "live", "%1", agentstore.StateThinking)

	_, err := manager.lifecycle.RestoreAgents(ctx, archived)
	require.ErrorIs(t, err, ErrTooManyAgents, "three back into a panel of three holding one is refused")

	for _, id := range archived {
		assert.NotNil(t, agentByID(t, st, id).ArchivedAt, "the refused batch stays in the archive whole")
	}

	restored, err := manager.lifecycle.RestoreAgents(ctx, archived[:2])
	require.NoError(t, err, "what does fit comes back")
	assert.Len(t, restored, 2)
}

// TestRestoreAgentsRefusesOnANameTaken covers a name being taken either by a live agent or by
// two of the returning agents between them.
func TestRestoreAgentsRefusesOnANameTaken(t *testing.T) {
	tests := []struct {
		name string

		liveNamesake bool

		secondNamesake bool
	}{
		{
			name:         "a live agent has taken the name",
			liveNamesake: true,
		},
		{
			name:           "two returning agents want one name",
			secondNamesake: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			manager := newManager(newFakeTmux(), st, "")
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			archived := seedInWorkflow(t, st, workflowID, "auth", "", agentstore.StateIdle)
			other := seedInWorkflow(t, st, workflowID, "api", "", agentstore.StateIdle)
			require.NoError(t, st.SoftDelete(ctx, "auth"))
			require.NoError(t, st.SoftDelete(ctx, "api"))

			batch := []string{archived.ID, other.ID}
			if tt.secondNamesake {
				second := seedInWorkflow(t, st, workflowID, "auth", "", agentstore.StateIdle)
				_, archiveErr := st.Agents.Archive(ctx, []string{second.ID})
				require.NoError(t, archiveErr)
				batch = append(batch, second.ID)
			}
			if tt.liveNamesake {
				seedInWorkflow(t, st, workflowID, "auth", "%1", agentstore.StateThinking)
			}

			_, err := manager.lifecycle.RestoreAgents(ctx, batch)

			require.ErrorIs(t, err, store.ErrNameTaken)
			require.ErrorContains(t, err, "auth", "the refusal names what is in the way")
			assert.NotNil(t, agentByID(t, st, other.ID).ArchivedAt, "the agent with no clash stays back too")
		})
	}
}

// TestRestoreAgentsAllowsANameSharedAcrossWorkflows pins that two agents called auth in two
// workflows are not a clash, since a name is only unique inside its own.
func TestRestoreAgentsAllowsANameSharedAcrossWorkflows(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	ctx := t.Context()

	backend, frontend := seedWorkflow(t, st), seedWorkflow(t, st)
	first := seedInWorkflow(t, st, backend, "auth", "", agentstore.StateIdle)
	second := seedInWorkflow(t, st, frontend, "auth", "", agentstore.StateIdle)

	_, archiveErr := st.Agents.Archive(ctx, []string{first.ID, second.ID})

	require.NoError(t, archiveErr)

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Len(t, restored, 2)

	for _, id := range []string{first.ID, second.ID} {
		assert.Nil(t, agentByID(t, st, id).ArchivedAt)
	}
}

// TestRestoreAgentsPassesOverTheLiveOnes pins live agents being skipped, since counting them
// against the ceiling would refuse a batch over slots they already hold.
func TestRestoreAgentsPassesOverTheLiveOnes(t *testing.T) {
	st := newStore(t)
	manager := st.agentServiceCapped(newFakeTmux(), &fakeLifecycle{}, 2)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	live := seedInWorkflow(t, st, workflowID, "live", "%1", agentstore.StateThinking)
	archived := seedInWorkflow(t, st, workflowID, "archived", "", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "archived"))

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{live.ID, archived.ID})
	require.NoError(t, err)
	require.Len(t, restored, 2)

	assert.Nil(t, agentByID(t, st, archived.ID).ArchivedAt)
	assert.Nil(t, agentByID(t, st, live.ID).ArchivedAt, "the live agent is returned as it stands")
}

// TestRestoreAgentsWithNothingArchived pins that a batch of already-live ids writes nothing
// rather than being refused for room it does not need.
func TestRestoreAgentsWithNothingArchived(t *testing.T) {
	st := newStore(t)
	manager := st.agentServiceCapped(newFakeTmux(), &fakeLifecycle{}, 2)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	first := seedInWorkflow(t, st, workflowID, "auth", "%1", agentstore.StateThinking)
	second := seedInWorkflow(t, st, workflowID, "api", "%2", agentstore.StateThinking)

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Len(t, restored, 2)
}

func seedRunningIn(t *testing.T, st *testStore, tmux paneStamper, workflowID, name, pane string) agentstore.Agent {
	t.Helper()

	agent := seedInWorkflow(t, st, workflowID, name, pane, agentstore.StateThinking)
	require.NoError(t, tmux.SetPaneOwner(pane, agent.ID))

	return agent
}

// TestArchiveAgentsNamesOnlyTheFirstMissing pins the refusal naming one id, since a hundred
// would put four kilobytes of uuids into a panel banner.
func TestArchiveAgentsNamesOnlyTheFirstMissing(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")

	ids := make([]string, 0, 20)
	for range 20 {
		ids = append(ids, uuid.NewString())
	}

	err := manager.lifecycle.ArchiveAgents(t.Context(), ids)

	require.ErrorIs(t, err, ErrAgentNotFound)
	require.ErrorContains(t, err, ids[0], "the first few are named")
	require.ErrorContains(t, err, "and 15 more", "and the rest are counted")
	assert.NotContains(t, err.Error(), ids[19], "rather than spelled out")
}
