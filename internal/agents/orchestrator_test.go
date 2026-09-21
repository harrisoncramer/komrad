package agents

import (
	"path/filepath"
	"testing"

	"weave/internal/caller"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStartOrchestratorLaunchesOne covers what a workflow's first orchestrator is given, in a
// workflow that hands every other agent a worktree and runs hooks over it.
func TestStartOrchestratorLaunchesOne(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	stateDir := t.TempDir()
	lifecycle := &fakeLifecycle{dir: t.TempDir()}
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		stateDir:  stateDir,
		lifecycle: lifecycle,
	})

	workflow, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "generous",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookPreLaunch, Script: "install-deps"}},
	})
	require.NoError(t, err)

	started, err := manager.orchestrator.Start(t.Context(), workflow.ID)
	require.NoError(t, err)

	assert.Equal(t, agentstore.RoleOrchestrator, started.Role)
	assert.Equal(t, workflow.ID, started.WorkflowID)
	assert.Nil(t, started.ParentAgentID, "it stands on its own rather than inside another agent")
	require.NotNil(t, started.ProvisionEnabled)
	assert.False(t, *started.ProvisionEnabled)
	assert.Equal(t, filepath.Join(stateDir, orchestratorDirName, workflow.ID), started.Cwd,
		"it runs in its own scratch directory rather than a worktree the workflow would have cut")
	assert.DirExists(t, started.Cwd)
	assert.NoDirExists(t, filepath.Join(started.Cwd, ".git"), "its directory is not a checkout")
	assert.Empty(t, lifecycle.ranHooks(), "the workflow's hooks are not run over a directory it does not own")
}

// TestStartOrchestratorRefusesAWorkflowThatIsNotThere pins that nothing is written for a
// workflow that could never hold an orchestrator.
func TestStartOrchestratorRefusesAWorkflowThatIsNotThere(t *testing.T) {
	tests := []struct {
		name       string
		workflowID string
	}{
		{
			name:       "an id that climbs out of the state directory",
			workflowID: "../../../../tmp/escaped",
		},
		{
			name:       "a well-formed id no workflow holds",
			workflowID: "00000000-0000-7000-8000-000000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			stateDir := t.TempDir()
			manager := st.agentService(agentServiceOptions{
				tmux:      newFakeTmux(),
				harnesses: fakeHarnesses{harness: &fakeHarness{}},
				addr:      "127.0.0.1:9999",
				stateDir:  stateDir,
			})

			_, err := manager.orchestrator.Start(t.Context(), tt.workflowID)
			require.ErrorIs(t, err, ErrWorkflowNotFound)

			assert.NoDirExists(t, filepath.Join(stateDir, orchestratorDirName),
				"no directory is made for a workflow that is not there")
		})
	}
}

// TestStartOrchestratorTakesTheOneAnotherCallerWon covers two panels asking at once, where the
// index refuses the second insert and the caller still asked for an orchestrator.
func TestStartOrchestratorTakesTheOneAnotherCallerWon(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		stateDir:  t.TempDir(),
	})
	ctx := t.Context()
	workflow := seedWorkflow(t, st)

	won, err := manager.orchestrator.Start(ctx, workflow)
	require.NoError(t, err)

	// The lookup Start opens with is what a racing caller misses, so the launch is driven
	// straight into the index instead.
	raced, err := manager.orchestrator.launchOrchestrator(ctx, workflow)
	require.NoError(t, err)
	assert.Equal(t, won.ID, raced.ID, "the conflict hands back the orchestrator that exists")
}

// TestStartOrchestratorReturnsTheOneTheWorkflowHas covers the second call, which has to reach
// the conversation already there rather than open another.
func TestStartOrchestratorReturnsTheOneTheWorkflowHas(t *testing.T) {
	tests := []struct {
		name    string
		archive bool
	}{
		{name: "a live orchestrator is handed back"},
		{name: "an archived orchestrator is brought back", archive: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: fakeHarnesses{harness: &fakeHarness{}},
				addr:      "127.0.0.1:9999",
				stateDir:  t.TempDir(),
			})
			ctx := t.Context()
			workflow := seedWorkflow(t, st)

			first, err := manager.orchestrator.Start(ctx, workflow)
			require.NoError(t, err)
			st.SeedTranscript(t, first.ID, "why this workflow was split three ways")

			if tt.archive {
				require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{first.ID}))
			}

			second, err := manager.orchestrator.Start(ctx, workflow)
			require.NoError(t, err)

			assert.Equal(t, first.ID, second.ID, "the workflow points at one orchestrator for life")
			assert.Nil(t, second.ArchivedAt, "and it is live again")

			stored, err := st.GetTranscript(ctx, second.ID)
			require.NoError(t, err)
			assert.Equal(t, "why this workflow was split three ways", stored,
				"what it said before is still there")
		})
	}
}

// TestOrchestratorSpawnsOnlyIntoItsOwnWorkflow pins the boundary an orchestrator is held to,
// since it stands behind one workflow rather than the panel.
func TestOrchestratorSpawnsOnlyIntoItsOwnWorkflow(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		stateDir:  t.TempDir(),
	})
	ctx := t.Context()

	mine, theirs := seedWorkflow(t, st), seedWorkflow(t, st)

	orchestrator, err := manager.orchestrator.Start(ctx, mine)
	require.NoError(t, err)

	spawning := caller.With(ctx, caller.Caller{AgentID: orchestrator.ID})

	_, err = manager.launch.NewAgent(spawning, NewAgentParams{
		Title:      "elsewhere",
		Prompt:     "do something in another workflow",
		WorkflowID: theirs,
	})
	require.ErrorIs(t, err, ErrOutsideWorkflow)

	own, err := manager.launch.NewAgent(spawning, NewAgentParams{
		Title:      "at home",
		Prompt:     "do something here",
		WorkflowID: mine,
	})
	require.NoError(t, err)
	assert.Equal(t, mine, own.WorkflowID)
}

// TestOrchestratorIsRefusedAReview pins that an agent writing no code cannot ask for its work
// to be read.
func TestOrchestratorIsRefusedAReview(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		stateDir:  t.TempDir(),
	})
	ctx := t.Context()

	orchestrator, err := manager.orchestrator.Start(ctx, seedWorkflow(t, st))
	require.NoError(t, err)

	_, _, err = manager.launch.RequestReview(ctx, RequestReviewParams{
		AgentID: orchestrator.ID,
		Prompt:  "read what I have not written",
	})
	require.ErrorIs(t, err, ErrReviewNeedsOwnWork)
}

// TestStartOrchestratorReplacesOneThatNeverRan pins the row a refused launch leaves behind: it
// holds no conversation, so resuming it asks its CLI for one that was never written.
func TestStartOrchestratorReplacesOneThatNeverRan(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		stateDir:  t.TempDir(),
	})
	ctx := t.Context()
	workflow := seedWorkflow(t, st)

	first, err := manager.orchestrator.Start(ctx, workflow)
	require.NoError(t, err)

	// What a launch the person refused leaves behind: the row stands and the reconciler takes
	// the pane away, so nothing on disk holds the conversation it was given an id for.
	forgotten, err := st.Agents.ForgetPane(ctx, agentstore.ForgetPaneParams{
		ID:        first.ID,
		Pane:      first.Pane,
		UpdatedAt: agentByID(t, st, first.ID).UpdatedAt,
	})
	require.NoError(t, err)
	require.True(t, forgotten)

	again, err := manager.orchestrator.Start(ctx, workflow)
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, again.ID, "the row that never spoke is replaced rather than resumed")
	assert.Equal(t, workflow, again.WorkflowID)
	assert.Equal(t, agentstore.RoleOrchestrator, again.Role)
}

// TestStartOrchestratorKeepsOneStillRunning pins that pressing the key twice does not replace
// the orchestrator the first press started, which has said nothing yet either.
func TestStartOrchestratorKeepsOneStillRunning(t *testing.T) {
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:      newFakeTmux(),
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		stateDir:  t.TempDir(),
	})
	ctx := t.Context()
	workflow := seedWorkflow(t, st)

	first, err := manager.orchestrator.Start(ctx, workflow)
	require.NoError(t, err)
	require.NotEmpty(t, first.Pane)

	again, err := manager.orchestrator.Start(ctx, workflow)
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID, "a running orchestrator is handed back, not replaced")
}

// TestStartOrchestratorKeepsOneThatHasSpoken is the other half: a row holding a conversation is
// resumed, never replaced.
func TestStartOrchestratorKeepsOneThatHasSpoken(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}},
		addr:      "127.0.0.1:9999",
		stateDir:  t.TempDir(),
	})
	ctx := t.Context()
	workflow := seedWorkflow(t, st)

	first, err := manager.orchestrator.Start(ctx, workflow)
	require.NoError(t, err)
	st.SeedTranscript(t, first.ID, "why this workflow was split three ways")

	again, err := manager.orchestrator.Start(ctx, workflow)
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)

	stored, err := st.GetTranscript(ctx, again.ID)
	require.NoError(t, err)
	assert.Equal(t, "why this workflow was split three ways", stored)
}
