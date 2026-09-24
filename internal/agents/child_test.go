package agents

import (
	"context"
	"testing"

	"weave/internal/harnesstest"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmuxtest"
	"weave/lib/xptr"
	"weave/lib/xslice"

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

// TestNewChildAgentRefusals pins each child a launch refuses and records nothing for.
func TestNewChildAgentRefusals(t *testing.T) {
	tests := []struct {
		name      string
		attachTo  func(t *testing.T, st *testStore, parent agentstore.Agent) agentstore.Agent
		params    func(parent agentstore.Agent) NewAgentParams
		wantErrIs error
	}{
		{
			name: "a directory of its own",
			params: func(parent agentstore.Agent) NewAgentParams {
				params := childOf(parent, "reviewer")
				params.Dir = "/tmp"

				return params
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a provisioner of its own",
			params: func(parent agentstore.Agent) NewAgentParams {
				params := childOf(parent, "reviewer")
				params.ProvisionEnabled = xptr.Ptr(true)

				return params
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a workflow other than its parent's",
			params: func(parent agentstore.Agent) NewAgentParams {
				params := childOf(parent, "reviewer")
				params.WorkflowID = "b9b1a1de-0000-7000-8000-000000000000"

				return params
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a parent nothing answers to",
			params: func(parent agentstore.Agent) NewAgentParams {
				params := childOf(parent, "reviewer")
				params.ParentAgentID = "b9b1a1de-0000-7000-8000-000000000000"

				return params
			},
			wantErrIs: ErrParentNotFound,
		},
		{
			name: "a parent already archived",
			attachTo: func(t *testing.T, st *testStore, parent agentstore.Agent) agentstore.Agent {
				t.Helper()
				require.NoError(t, st.SoftDelete(t.Context(), parent.Name))

				return parent
			},
			wantErrIs: ErrAgentArchived,
		},
		{
			name: "a parent with no directory",
			attachTo: func(t *testing.T, st *testStore, parent agentstore.Agent) agentstore.Agent {
				t.Helper()
				require.NoError(t, st.Update(t.Context(), withCwd(parent, "")))

				return parent
			},
			wantErrIs: ErrChildNeedsParentDirectory,
		},
		{
			name: "a parent that is itself a child",
			attachTo: func(t *testing.T, st *testStore, parent agentstore.Agent) agentstore.Agent {
				t.Helper()

				return seedChild(t, st, tmuxtest.NewFake(), parent)
			},
			wantErrIs: ErrParentIsChild,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")

			parent := withCwd(seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle), t.TempDir())
			require.NoError(t, st.Update(t.Context(), parent))
			if tt.attachTo != nil {
				parent = tt.attachTo(t, st, parent)
			}

			params := childOf(parent, "second-reviewer")
			if tt.params != nil {
				params = tt.params(parent)
			}

			before, err := st.ListAll(t.Context())
			require.NoError(t, err)

			created, err := manager.launch.NewAgent(t.Context(), params)
			require.ErrorIs(t, err, tt.wantErrIs)
			assert.Nil(t, created)

			after, err := st.ListAll(t.Context())
			require.NoError(t, err)
			assert.Len(t, after, len(before), "the refusal records nothing")
		})
	}
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

// TestTheFamilyMovesTogether pins that a parent's archive, delete and restore carry its children
// and a child's own leave the parent.
func TestTheFamilyMovesTogether(t *testing.T) {
	tests := []struct {
		name     string
		act      func(ctx context.Context, lifecycle *LifecycleService, parent, child agentstore.Agent) error
		wantLive []string
		wantKept []string
	}{
		{
			name: "archiving the parent takes the child",
			act: func(ctx context.Context, lifecycle *LifecycleService, parent, _ agentstore.Agent) error {
				return lifecycle.ArchiveAgents(ctx, []string{parent.ID})
			},
			wantKept: []string{"author", "reviewer"},
		},
		{
			name: "archiving both is the same archive",
			act: func(ctx context.Context, lifecycle *LifecycleService, parent, child agentstore.Agent) error {
				return lifecycle.ArchiveAgents(ctx, []string{parent.ID, child.ID})
			},
			wantKept: []string{"author", "reviewer"},
		},
		{
			name: "archiving the child leaves the parent",
			act: func(ctx context.Context, lifecycle *LifecycleService, _, child agentstore.Agent) error {
				return lifecycle.ArchiveAgents(ctx, []string{child.ID})
			},
			wantLive: []string{"author"},
			wantKept: []string{"author", "reviewer"},
		},
		{
			name: "deleting the parent takes the child",
			act: func(ctx context.Context, lifecycle *LifecycleService, parent, _ agentstore.Agent) error {
				return lifecycle.DeleteAgents(ctx, []string{parent.ID})
			},
		},
		{
			name: "deleting the child leaves the parent",
			act: func(ctx context.Context, lifecycle *LifecycleService, _, child agentstore.Agent) error {
				return lifecycle.DeleteAgents(ctx, []string{child.ID})
			},
			wantLive: []string{"author"},
			wantKept: []string{"author"},
		},
		{
			name: "restoring the parent brings the child back",
			act: func(ctx context.Context, lifecycle *LifecycleService, parent, _ agentstore.Agent) error {
				if err := lifecycle.ArchiveAgents(ctx, []string{parent.ID}); err != nil {
					return err
				}

				_, err := lifecycle.RestoreAgents(ctx, []string{parent.ID})

				return err
			},
			wantLive: []string{"author", "reviewer"},
			wantKept: []string{"author", "reviewer"},
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

			require.NoError(t, tt.act(ctx, manager.lifecycle, parent, child))

			live, err := st.List(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantLive, agentNames(live))

			kept, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantKept, agentNames(kept))
			assert.Contains(t, tmux.KilledPanes, child.Pane, "the child's pane goes with its row")
		})
	}
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

func agentNames(agents []agentstore.Agent) []string {
	return xslice.Map(agents, func(agent agentstore.Agent) string {
		return agent.Name
	})
}
