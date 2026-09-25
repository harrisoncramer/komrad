package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/harnesstest"
	"komrad/internal/scripts"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tmuxtest"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgentRunsPreLaunchHooksInOrder covers the pair this table exists for: a worktree
// workflow that also wants something done inside the worktree before the CLI starts.
func TestNewAgentRunsPreLaunchHooksInOrder(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	provisioned := t.TempDir()
	lifecycle := &fakeLifecycle{dir: provisioned}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookPreLaunch, Script: "copy-env"},
			{Event: workflowstore.HookPostLaunch, Script: "announce"},
		},
	})
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, agent)

	ran := lifecycle.ranHooks()
	require.GreaterOrEqual(t, len(ran), 2)
	assert.Equal(t, ranHook{
		Event:  workflowstore.HookPreLaunch,
		Script: "install-deps",
		Cwd:    provisioned,
	}, ran[0],
		"the first pre-launch hook runs in the directory the provisioner named")
	assert.Equal(t, ranHook{
		Event:  workflowstore.HookPreLaunch,
		Script: "copy-env",
		Cwd:    provisioned,
	}, ran[1],
		"and the second runs after it, in the order the workflow lists them")
}

// TestNewAgentRunsPostLaunchHooksInTheBackground pins the other half of the pair: the call does
// not wait for them.
func TestNewAgentRunsPostLaunchHooksInTheBackground(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{dir: t.TempDir()}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:  "backend",
		Hooks: []workflowstore.Hook{{Event: workflowstore.HookPostLaunch, Script: "announce"}},
	})
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, agent)

	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) == 1
	}, 5*time.Second, 10*time.Millisecond, "the post-launch hook runs once the pane is up")

	ran := lifecycle.ranHooks()
	assert.Equal(t, workflowstore.HookPostLaunch, ran[0].Event)
	assert.Equal(t, "announce", ran[0].Script)
}

// TestNewAgentAbandonsACreateAPreLaunchHookRefused pins a failing pre-launch hook taking the
// create with it.
func TestNewAgentAbandonsACreateAPreLaunchHookRefused(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{
		dir:      t.TempDir(),
		hookErrs: map[string]error{"install-deps": errors.New("mise refused")},
	}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookPreLaunch, Script: "copy-env"},
		},
	})
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.Error(t, err)
	assert.Nil(t, agent)

	ran := lifecycle.ranHooks()
	require.Len(t, ran, 1, "the hooks after the one that failed are skipped")
	assert.Equal(t, "install-deps", ran[0].Script)
	assert.Equal(t, []string{worktreeDeprovisioner}, lifecycle.deprovision, "what the provisioner built is given back")

	agents, listErr := st.Agents.List(t.Context())
	require.NoError(t, listErr)
	require.Len(t, agents, 1)
	assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State)
}

// TestNewChildAgentRunsNoHooks pins a child running no moment of its own.
func TestNewChildAgentRunsNoHooks(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{dir: t.TempDir()}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:  "backend",
		Hooks: []workflowstore.Hook{{Event: workflowstore.HookPreLaunch, Script: "install-deps"}},
	})
	require.NoError(t, err)

	parent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "author",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	before := len(lifecycle.ranHooks())

	child, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:         "reviewer",
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, child)

	assert.Len(t, lifecycle.ranHooks(), before, "no moment fires for a child")
}

// TestARetiredProvisionerRunsNoHooks pins the other half of the ownership rule: an agent handed
// somebody else's checkout.
func TestARetiredProvisionerRunsNoHooks(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{dir: t.TempDir()}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookPostLaunch, Script: "announce"},
		},
	})
	require.NoError(t, err)

	elsewhere := t.TempDir()
	agent, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:            "reviewer",
		Dir:              elsewhere,
		WorkflowID:       created.ID,
		ProvisionEnabled: xptr.Ptr(false),
	})
	require.NoError(t, err)
	require.NotNil(t, agent)

	assert.Equal(t, elsewhere, agent.Cwd, "it keeps the directory it was given")
	assert.Empty(t, lifecycle.provision, "nothing was provisioned")
	assert.Empty(t, lifecycle.ranHooks(), "so no moment fires against it")
}

// TestNewWorkflowRefusesAHookOnAMomentKomradDoesNotFire pins the refusal a hook hung on a moment
// nothing fires is answered with.
func TestNewWorkflowRefusesAHookOnAMomentKomradDoesNotFire(t *testing.T) {
	st := newStore(t)
	workflows := newWorkflowService(st, tmuxtest.NewFake())

	_, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name:  "backend",
		Hooks: []workflowstore.Hook{{Event: "pre-lunch", Script: "install-deps"}},
	})

	require.ErrorIs(t, err, ErrUnknownHookEvent)
}

// TestWorkflowHooksRoundTripInOrder pins the hooks coming back as they were written, in the
// order they run.
func TestWorkflowHooksRoundTripInOrder(t *testing.T) {
	st := newStore(t)
	workflows := newWorkflowService(st, tmuxtest.NewFake())

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookPreLaunch, Script: "copy-env"},
			{Event: workflowstore.HookPostLaunch, Script: "announce"},
			{Event: workflowstore.HookPreLaunch, Script: ""},
		},
	})
	require.NoError(t, err)

	got, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, []workflowstore.Hook{
		{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
		{Event: workflowstore.HookPreLaunch, Script: "copy-env"},
		{Event: workflowstore.HookPostLaunch, Script: "announce"},
	}, got.Hooks, "the blank a None select sends is dropped rather than stored")

	require.NoError(t, workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookPostLaunch, Script: "announce"}},
	}))

	updated, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, []workflowstore.Hook{{Event: workflowstore.HookPostLaunch, Script: "announce"}}, updated.Hooks,
		"an update writes the whole list rather than adding to it")
}

// TestResumeFiresPostResumeHooks pins the moment a dormant agent comes back in.
func TestResumeFiresPostResumeHooks(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookPostResume, Script: "announce-back"},
		},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	agent.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, agent))

	_, err = manager.resume.ResumeAgent(ctx, agent.ID)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) == 1
	}, 5*time.Second, 10*time.Millisecond, "the post-resume hook runs once the pane is back")

	ran := lifecycle.ranHooks()
	assert.Equal(t, workflowstore.HookPostResume, ran[0].Event)
	assert.Equal(t, "announce-back", ran[0].Script, "and pre-launch does not rerun")
}

// TestRestoreFiresPostRestoreHooks pins the moment the row comes back.
func TestRestoreFiresPostRestoreHooks(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookPostRestore, Script: "unlock"}},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "auth"))

	_, err = manager.lifecycle.RestoreAgents(ctx, []string{agent.ID})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) == 1
	}, 5*time.Second, 10*time.Millisecond, "the post-restore hook runs once the row is back")

	assert.Equal(t, workflowstore.HookPostRestore, lifecycle.ranHooks()[0].Event)
}

// TestAProvisionerDisabledAgentRunsNoHooksOnTheWayBackIn pins the moments off the create path
// respecting the same rule the create path does: an agent that kept the directory it was given
// is working in somebody else's tree, and a hook there would write into it.
func TestAProvisionerDisabledAgentRunsNoHooksOnTheWayBackIn(t *testing.T) {
	tests := []struct {
		name string
		fire func(t *testing.T, manager *testAgents, agent agentstore.Agent)
	}{
		{
			name: "a resume",
			fire: func(t *testing.T, manager *testAgents, agent agentstore.Agent) {
				t.Helper()

				_, err := manager.resume.ResumeAgent(t.Context(), agent.ID)
				require.NoError(t, err)
			},
		},
		{
			name: "a restore",
			fire: func(t *testing.T, manager *testAgents, agent agentstore.Agent) {
				t.Helper()

				_, err := manager.lifecycle.RestoreAgents(t.Context(), []string{agent.ID})
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			ctx := t.Context()

			lifecycle := &fakeLifecycle{}
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

			created, err := st.Workflows.Create(ctx, workflowstore.Config{
				Name:     "backend",
				Cwd:      fakeCheckout(t),
				Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
				Hooks: []workflowstore.Hook{
					{Event: workflowstore.HookPostResume, Script: "announce-back"},
					{Event: workflowstore.HookPostRestore, Script: "unlock"},
				},
			})
			require.NoError(t, err)

			require.NoError(t, st.createAgent(ctx, agentstore.Agent{
				Name:             "reviewer",
				Pane:             "%0",
				State:            agentstore.StateIdle,
				Cwd:              t.TempDir(),
				WorkflowID:       created.ID,
				ProvisionEnabled: xptr.Ptr(false),
			}))
			agent, err := st.Agents.GetInWorkflow(ctx, created.ID, "reviewer")
			require.NoError(t, err)

			if tt.name == "a restore" {
				require.NoError(t, st.SoftDelete(ctx, "reviewer"))
			}

			tt.fire(t, manager, *agent)

			// Long enough for a hook chain to have started if one was going to.
			require.Never(t, func() bool {
				return len(lifecycle.ranHooks()) > 0
			}, 300*time.Millisecond, 20*time.Millisecond, "no moment fires against a checkout the agent does not own")
		})
	}
}

// TestRestoreFiresPostRestoreOnlyOnWhatWasArchived pins the set a restore owes its hooks, which
// leaves out an agent in the batch that was already running.
func TestRestoreFiresPostRestoreOnlyOnWhatWasArchived(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookPostRestore, Script: "unlock"}},
	})
	require.NoError(t, err)

	archived := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	archived.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, archived))

	live := seedInWorkflow(t, st, created.ID, "billing", "%1", agentstore.StateThinking)
	live.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, live))

	require.NoError(t, st.SoftDelete(ctx, "auth"))

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{archived.ID, live.ID})
	require.NoError(t, err)
	assert.Len(t, restored, 2, "an agent that was not archived still comes back as it stands")

	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) > 0
	}, 5*time.Second, 10*time.Millisecond, "the archived agent's post-restore hook runs")

	// Long enough for a second chain to have started if one was going to.
	require.Never(t, func() bool {
		return len(lifecycle.ranHooks()) > 1
	}, 300*time.Millisecond, 20*time.Millisecond, "and the one that was never archived is owed nothing")

	assert.Equal(t, archived.Cwd, lifecycle.ranHooks()[0].Cwd, "the hook ran in the restored agent's directory")
}

// TestRestoreWorkflowFiresPostRestoreHooks pins the sibling path owing the same thing.
func TestRestoreWorkflowFiresPostRestoreHooks(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	lifecycle := &fakeLifecycle{}
	workflows := st.workflowService(workflowServiceOptions{tmux: tmux, lifecycle: lifecycle})

	created, err := workflows.NewWorkflow(ctx, workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookPostRestore, Script: "unlock"}},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	agent.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, agent))

	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))

	_, err = workflows.RestoreWorkflow(ctx, created.ID)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) > 0
	}, 5*time.Second, 10*time.Millisecond, "the post-restore hook runs for the agent inside")

	assert.Equal(t, []ranHook{{
		Event:  workflowstore.HookPostRestore,
		Script: "unlock",
		Cwd:    agent.Cwd,
	}}, lifecycle.ranHooks())
}

// TestRunDeprovisionLogsOneThatWouldNotStart covers the deprovision that never becomes a process at
// all.
func TestRunDeprovisionLogsOneThatWouldNotStart(t *testing.T) {
	lifecycle := &fakeLifecycle{deprovisionStartErr: scripts.ErrScriptNotFound}
	ctx, records := loggedContext(t)

	runDeprovision(ctx, lifecycle, agentstore.Agent{ID: "a1", Cwd: "/repo/.worktrees/auth"}, "was-renamed", nil)

	got := recordFor(t, records(), "the deprovisioner would not start")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, "was-renamed", got["komrad.script.name"])
	assert.Equal(t, "/repo/.worktrees/auth", got["komrad.script.agent.cwd"])
}

// TestDeprovisionerLogsAWorkflowThatWouldNotRead covers the read that breaks rather than
// answering, which skips a teardown as quietly as a workflow naming none.
func TestDeprovisionerLogsAWorkflowThatWouldNotRead(t *testing.T) {
	st := newStore(t)
	workflowID := seedWorkflow(t, st)
	logged, records := loggedContext(t)
	ctx, cancel := context.WithCancel(logged)
	cancel()

	agent := agentstore.Agent{
		ID:         "a1",
		Cwd:        "/repo/.worktrees/auth",
		WorkflowID: workflowID,
	}
	assert.Empty(t, deprovisionerFor(ctx, st.Workflows, agent))

	got := recordFor(t, records(), "the workflow behind an agent could not be read")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, workflowID, got["komrad.workflow.id"])
	assert.Equal(t, "a1", got["komrad.script.agent.id"])
	assert.Equal(t, "/repo/.worktrees/auth", got["komrad.script.agent.cwd"])
}

// TestDeprovisionerSaysNothingWhenThereIsNothingToTearDown pins that no teardown is named and
// nothing is logged.
func TestDeprovisionerSaysNothingWhenThereIsNothingToTearDown(t *testing.T) {
	tests := []struct {
		name       string
		workflowID func(t *testing.T, st *testStore) string
	}{
		{
			name: "a workflow no row holds",
			workflowID: func(*testing.T, *testStore) string {
				return "b9b1a1de-0000-7000-8000-000000000000"
			},
		},
		{
			name:       "a workflow that makes no worktrees",
			workflowID: seedWorkflow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			agent := agentstore.Agent{
				ID:         "a1",
				WorkflowID: tt.workflowID(t, st),
			}
			ctx, records := loggedContext(t)

			assert.Empty(t, deprovisionerFor(ctx, st.Workflows, agent))
			assert.Empty(t, records())
		})
	}
}

// TestTheWorktreeToggleIsTheWholeProvisioner pins what a workflow can say about an agent's
// directory now that it cannot name a script: the built-in pair when the toggle is on, and
// nothing at all otherwise.
func TestTheWorktreeToggleIsTheWholeProvisioner(t *testing.T) {
	tests := []struct {
		name            string
		worktree        bool
		provision       *bool
		wantProvision   string
		wantDeprovision string
	}{
		{
			name:     "a workflow that provisions nothing has nothing to run either way",
			worktree: false,
		},
		{
			name:            "the toggle on names the built-in pair",
			worktree:        true,
			wantProvision:   worktreeProvisioner,
			wantDeprovision: worktreeDeprovisioner,
		},
		{
			name:      "an agent told to keep its own directory runs neither half",
			worktree:  true,
			provision: xptr.Ptr(false),
		},
		{
			name:            "an agent following its workflow runs both",
			worktree:        true,
			provision:       xptr.Ptr(true),
			wantProvision:   worktreeProvisioner,
			wantDeprovision: worktreeDeprovisioner,
		},
		{
			name:      "the override cannot conjure a worktree a workflow does not make",
			worktree:  false,
			provision: xptr.Ptr(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			config := worktreeConfig(t, "backend")
			config.Worktree.Enabled = tt.worktree
			workflow, err := st.Workflows.Create(t.Context(), config)
			require.NoError(t, err)

			agent := agentstore.Agent{
				ID:               "a1",
				WorkflowID:       workflow.ID,
				ProvisionEnabled: tt.provision,
			}

			assert.Equal(t, tt.wantProvision, provisionerFor(t.Context(), st.Workflows, agent))
			assert.Equal(t, tt.wantDeprovision, deprovisionerFor(t.Context(), st.Workflows, agent))
		})
	}
}
