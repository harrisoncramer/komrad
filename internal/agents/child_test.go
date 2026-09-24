package agents

import (
	"testing"

	"weave/internal/harnesstest"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmuxtest"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewChildAgentTakesItsLaunchFromTheParent pins what a child is given rather than asked
// for: the parent's checkout, no provisioner of its own, and the parent's workflow.
func TestNewChildAgentTakesItsLaunchFromTheParent(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	dir := t.TempDir()
	parent, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        dir,
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	child, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:         "reviewer",
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, child)

	assert.Equal(t, dir, child.Cwd, "a child runs where its parent runs")
	require.NotNil(t, child.ParentAgentID)
	assert.Equal(t, parent.ID, *child.ParentAgentID)
	assert.Equal(t, parent.WorkflowID, child.WorkflowID)

	// An unset provisioner falls back to the workflow's, which for a worktree-enabled workflow
	// would tear the parent's checkout down, so the blank has to be on the row itself.
	require.NotNil(t, child.ProvisionEnabled)
	assert.False(t, *child.ProvisionEnabled)

	assert.Empty(t, deprovisionerFor(ctx, st.Workflows, *child), "and a provisioner turned off leaves nothing to run against it")
}

// TestNewChildAgentRefusals pins that a request asking for what the parent decides is answered
// rather than quietly overridden.
func TestNewChildAgentRefusals(t *testing.T) {
	tests := []struct {
		name      string
		params    func(parent agentstore.Agent) NewAgentParams
		wantErrIs error
	}{
		{
			name: "a directory of its own",
			params: func(parent agentstore.Agent) NewAgentParams {
				return NewAgentParams{
					Dir:           "/tmp",
					WorkflowID:    parent.WorkflowID,
					ParentAgentID: parent.ID,
				}
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a provisioner of its own",
			params: func(parent agentstore.Agent) NewAgentParams {
				return NewAgentParams{
					ProvisionEnabled: xptr.Ptr(true),
					WorkflowID:       parent.WorkflowID,
					ParentAgentID:    parent.ID,
				}
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a workflow other than its parent's",
			params: func(parent agentstore.Agent) NewAgentParams {
				return NewAgentParams{WorkflowID: "b9b1a1de-0000-7000-8000-000000000000", ParentAgentID: parent.ID}
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a parent nothing answers to",
			params: func(parent agentstore.Agent) NewAgentParams {
				return NewAgentParams{WorkflowID: parent.WorkflowID, ParentAgentID: "b9b1a1de-0000-7000-8000-000000000000"}
			},
			wantErrIs: ErrParentNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")

			parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
			require.NoError(t, st.Update(t.Context(), withCwd(parent, t.TempDir())))

			params := tt.params(parent)
			params.Prompt = "read the change"
			params.Title = "reviewer"

			created, err := manager.launch.NewAgent(t.Context(), params)
			require.ErrorIs(t, err, tt.wantErrIs)
			assert.Nil(t, created)

			live, listErr := st.List(t.Context())
			require.NoError(t, listErr)
			assert.Len(t, live, 1, "the refusal records nothing beside the parent")
		})
	}
}

// TestNewChildAgentRefusesAnArchivedParent pins that a child is never attached to an agent
// already on its way out.
func TestNewChildAgentRefusesAnArchivedParent(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{parent.ID}))

	_, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:         "reviewer",
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})
	require.ErrorIs(t, err, ErrAgentArchived)
}

// TestNewChildAgentRefusesAParentWithNoDirectory pins that a child is never launched into the
// workflow's own checkout.
func TestNewChildAgentRefusesAParentWithNoDirectory(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.Empty(t, parent.Cwd)

	_, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:         "reviewer",
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})
	require.ErrorIs(t, err, ErrChildNeedsParentDirectory)
}

// TestNewChildAgentRefusesAChildOfAChild pins the one level.
func TestNewChildAgentRefusesAChildOfAChild(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	_, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:         "second-reviewer",
		Prompt:        "read the review",
		WorkflowID:    child.WorkflowID,
		ParentAgentID: child.ID,
	})
	require.ErrorIs(t, err, ErrParentIsChild)
}

// TestNewChildAgentUnderTheChildCeiling pins the per-parent cap.
func TestNewChildAgentUnderTheChildCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:        tmux,
		harnesses:   harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:        "127.0.0.1:9999",
		maxChildren: 1,
	})
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, st.Update(ctx, withCwd(parent, t.TempDir())))

	attach := func(title string) error {
		_, err := manager.launch.NewAgent(ctx, childOf(parent, title))

		return err
	}

	require.NoError(t, attach("reviewer"))
	require.ErrorIs(t, attach("second-reviewer"), ErrTooManyChildren)
}

// TestRestoreOfAChildUnderTheChildCeiling pins that a child comes back only while its parent
// still has room.
func TestRestoreOfAChildUnderTheChildCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:        tmux,
		harnesses:   harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:        "127.0.0.1:9999",
		maxChildren: 1,
	})
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, st.Update(ctx, withCwd(parent, t.TempDir())))

	first, err := manager.launch.NewAgent(ctx, childOf(parent, "reviewer"))
	require.NoError(t, err)
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{first.ID}))

	_, err = manager.launch.NewAgent(ctx, childOf(parent, "second-reviewer"))
	require.NoError(t, err, "the ceiling frees up once one child is archived")

	_, err = manager.lifecycle.RestoreAgents(ctx, []string{first.ID})
	require.ErrorIs(t, err, ErrTooManyChildren)
	assert.NotNil(t, agentByID(t, st, first.ID).ArchivedAt, "the refusal restores nothing")

	held, err := st.Agents.CountLiveChildren(ctx, parent.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, held)
}

// TestRestoreOfAWorkflowUnderTheChildCeiling pins the same ceiling on the other way back, for a
// workflow archived holding two children of one parent.
func TestRestoreOfAWorkflowUnderTheChildCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:        tmux,
		harnesses:   harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:        "127.0.0.1:9999",
		maxChildren: 1,
	})
	workflows := st.workflowService(workflowServiceOptions{tmux: tmux, maxChildren: 1})
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, st.Update(ctx, withCwd(parent, t.TempDir())))

	first, err := manager.launch.NewAgent(ctx, childOf(parent, "reviewer"))
	require.NoError(t, err)
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{first.ID}))

	_, err = manager.launch.NewAgent(ctx, childOf(parent, "second-reviewer"))
	require.NoError(t, err)

	require.NoError(t, workflows.ArchiveWorkflow(ctx, parent.WorkflowID))

	_, err = workflows.RestoreWorkflow(ctx, parent.WorkflowID)
	require.ErrorIs(t, err, ErrTooManyChildren)

	held, err := st.Agents.CountLiveChildren(ctx, parent.ID)
	require.NoError(t, err)
	assert.Zero(t, held, "the refusal restores nothing")
}

func childOf(parent agentstore.Agent, title string) NewAgentParams {
	return NewAgentParams{
		Title:         title,
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	}
}

func withCwd(agent agentstore.Agent, dir string) agentstore.Agent {
	agent.Cwd = dir

	return agent
}

// TestArchiveTakesTheChildren pins that a child goes with its parent, whether or not the call
// named it too.
func TestArchiveTakesTheChildren(t *testing.T) {
	tests := []struct {
		name    string
		archive func(parent, child agentstore.Agent) []string
	}{
		{
			name:    "the parent takes the child with it",
			archive: func(parent, _ agentstore.Agent) []string { return []string{parent.ID} },
		},
		{
			name:    "naming both is the same archive",
			archive: func(parent, child agentstore.Agent) []string { return []string{parent.ID, child.ID} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
			child := seedChild(t, st, tmux, parent)

			require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, tt.archive(parent, child)))

			live, err := st.List(ctx)
			require.NoError(t, err)
			assert.Empty(t, live, "the family goes together")
			assert.Contains(t, tmux.KilledPanes, child.Pane, "and the child's pane goes with its row")
		})
	}
}

// TestArchiveOfAChildLeavesTheParent pins that finishing with one child leaves the agent it was
// reading for where it was.
func TestArchiveOfAChildLeavesTheParent(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{child.ID}))

	live, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1, "only the child goes")
	assert.Equal(t, parent.ID, live[0].ID)
	assert.Contains(t, tmux.KilledPanes, child.Pane)
}

// TestDeleteTakesTheChildren pins that deleting a parent removes the children too, and that
// their panes are dead before the parent's cleanup runs against the checkout they shared.
func TestDeleteTakesTheChildren(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{parent.ID}))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, all, "nothing is left of the family")
	assert.Contains(t, tmux.KilledPanes, child.Pane)
}

// TestDeleteTakesAChildAlone pins that a child can be destroyed by itself.
func TestDeleteTakesAChildAlone(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{child.ID}))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "the parent is left as it was")
	assert.Equal(t, parent.ID, all[0].ID)
	assert.Contains(t, tmux.KilledPanes, child.Pane)
}

// TestRestoreBringsTheChildrenBack pins that a parent taken out of the archive arrives with the
// children it was archived with.
func TestRestoreBringsTheChildrenBack(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	child := seedChild(t, st, tmux, parent)

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{parent.ID}))

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{parent.ID})
	require.NoError(t, err)
	assert.Len(t, restored, 2)

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Len(t, live, 2, "the family comes back together")

	assert.Equal(t, child.ID, agentByID(t, st, child.ID).ID)
	assert.Nil(t, agentByID(t, st, child.ID).ArchivedAt)
}

// TestRestoreOfAChildNeedsItsParentBack pins the one direction the family rule still holds in:
// a child comes back only once there is a live row for it to be drawn under.
func TestRestoreOfAChildNeedsItsParentBack(t *testing.T) {
	tests := []struct {
		name        string
		restoreBoth bool
		wantErrIs   error
	}{
		{
			name:        "the parent is live again",
			restoreBoth: true,
		},
		{
			name:      "the parent is still archived",
			wantErrIs: ErrParentStillArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
			child := seedChild(t, st, tmux, parent)

			require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{parent.ID}))
			if tt.restoreBoth {
				_, err := manager.lifecycle.RestoreAgents(ctx, []string{parent.ID})
				require.NoError(t, err)
				require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, []string{child.ID}))
			}

			restored, err := manager.lifecycle.RestoreAgents(ctx, []string{child.ID})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.NotNil(t, agentByID(t, st, child.ID).ArchivedAt, "the refusal restores nothing")

				return
			}

			require.NoError(t, err)
			assert.Len(t, restored, 1)
			assert.Nil(t, agentByID(t, st, child.ID).ArchivedAt)
		})
	}
}

// TestChildBriefingReplacesTheDelegateOne pins what a child is told.
func TestChildBriefingReplacesTheDelegateOne(t *testing.T) {
	launcher := NewLauncher(NewLauncherParams{
		Tmux:    tmuxtest.NewFake(),
		Addr:    "127.0.0.1:9999",
		LogPath: briefedLogPath,
	})

	prompt := launcher.appendedSystemPrompt(launchParams{
		SpawnedByAgentID: "agent-parent",
		SpawnedByName:    "author",
		ParentAgentID:    "agent-parent",
		ParentName:       "author",
	})

	assert.Contains(t, prompt, "You are a child of agent author (agent-parent)")
	assert.Contains(t, prompt, "do not commit")
	assert.Contains(t, prompt, "you go when the parent goes")
	assert.NotContains(t, prompt, "You were spawned by agent",
		"the spawner paragraph would understate what a child runs under")
}

// TestNewChildAgentRefusesAnOrchestratorParent pins that the agent folded onto a workflow's own row
// holds no children.
func TestNewChildAgentRefusesAnOrchestratorParent(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	parent := seedOrchestrator(t, st, tmux)
	require.NoError(t, st.Update(ctx, withCwd(parent, t.TempDir())))

	created, err := manager.launch.NewAgent(ctx, childOf(parent, "reader"))
	require.ErrorIs(t, err, ErrParentIsOrchestrator)
	assert.Nil(t, created)
	require.ErrorContains(t, err, "provisionEnabled false", "the refusal names the shape to use instead")

	live, err := st.List(ctx)
	require.NoError(t, err)
	assert.Len(t, live, 1, "the refusal records nothing beside the orchestrator")
}

// seedOrchestrator records the agent folded onto a workflow's own row.
func seedOrchestrator(t *testing.T, st *testStore, tmux paneStamper) agentstore.Agent {
	t.Helper()
	ctx := t.Context()

	const name = "orchestrator"
	workflowID := seedWorkflow(t, st)

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:       name,
		Pane:       "%" + name,
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Role:       agentstore.RoleOrchestrator,
	}))

	got, err := st.Agents.GetInWorkflow(ctx, workflowID, name)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, tmux.SetPaneOwner(got.Pane, got.ID))

	return *got
}
