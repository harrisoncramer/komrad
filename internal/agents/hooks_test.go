package agents

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"komrad/internal/harnesstest"
	"komrad/internal/scripts"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tmuxtest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

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

// TestAFailedPreResumeRefusesTheResume covers a pre hook vetoing the way back in.
func TestAFailedPreResumeRefusesTheResume(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	lifecycle := &fakeLifecycle{hookErrs: map[string]error{"warm-up": errors.New("no slot free")}}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreResume, Script: "warm-up"},
			{Event: workflowstore.HookPostResume, Script: "announce-back"},
		},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	agent.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, agent))

	_, err = manager.resume.ResumeAgent(ctx, agent.ID)
	require.ErrorContains(t, err, "no slot free")
	assert.Contains(t, err.Error(), `"auth"`, "the refusal names the agent")

	require.Never(t, func() bool {
		return len(lifecycle.ranHooks()) > 1
	}, 300*time.Millisecond, 20*time.Millisecond, "post-resume never follows a refused resume")

	assert.Equal(t, []ranHook{{
		Event:  workflowstore.HookPreResume,
		Script: "warm-up",
		Cwd:    agent.Cwd,
	}}, lifecycle.ranHooks())

	after, err := st.Agents.GetByID(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, agentstore.StateIdle, after.State, "the row goes back to the state it was in")
}

type archiveCall struct {
	name    string
	archive func(t *testing.T, st *testStore, lifecycle *fakeLifecycle, workflowID string, agents []agentstore.Agent) error
}

func archiveCalls() []archiveCall {
	return []archiveCall{
		{
			name: "archiving the agents",
			archive: func(t *testing.T, st *testStore, lifecycle *fakeLifecycle, _ string, agents []agentstore.Agent) error {
				t.Helper()

				manager := st.agentServiceWithLifecycle(tmuxtest.NewFake(), harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

				return manager.lifecycle.ArchiveAgents(t.Context(), agentstore.IDs(agents))
			},
		},
		{
			name: "archiving the workflow",
			archive: func(t *testing.T, st *testStore, lifecycle *fakeLifecycle, workflowID string, _ []agentstore.Agent) error {
				t.Helper()

				workflows := st.workflowService(workflowServiceOptions{tmux: tmuxtest.NewFake(), lifecycle: lifecycle})

				return workflows.ArchiveWorkflow(t.Context(), workflowID)
			},
		},
	}
}

func seedHookedAgents(t *testing.T, st *testStore, hooks []workflowstore.Hook, names ...string) (string, []agentstore.Agent) {
	t.Helper()
	ctx := t.Context()

	created, err := st.Workflows.Create(ctx, workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks:    hooks,
	})
	require.NoError(t, err)

	agents := make([]agentstore.Agent, 0, len(names))
	for i, name := range names {
		agent := seedInWorkflow(t, st, created.ID, name, "%"+string(rune('0'+i)), agentstore.StateIdle)
		agent.Cwd = t.TempDir()
		require.NoError(t, st.Agents.Update(ctx, agent))
		agents = append(agents, agent)
	}

	return created.ID, agents
}

func ranEvents(lifecycle *fakeLifecycle) []string {
	return xslice.Map(lifecycle.ranHooks(), func(ran ranHook) string { return ran.Event })
}

// TestArchiveHooks covers both moments around an archive, through both calls that archive.
func TestArchiveHooks(t *testing.T) {
	refused := []string{
		workflowstore.HookPreArchive,
		workflowstore.HookPreArchive,
	}
	archived := []string{
		workflowstore.HookPreArchive,
		workflowstore.HookPreArchive,
		workflowstore.HookPostArchive,
		workflowstore.HookPostArchive,
	}
	postOnly := []string{
		workflowstore.HookPostArchive,
		workflowstore.HookPostArchive,
	}
	byAgents, byWorkflow := archiveCalls()[0], archiveCalls()[1]

	tests := []struct {
		name         string
		call         archiveCall
		hookErrs     map[string]error
		removeDir    bool
		wantErr      string
		wantArchived bool
		wantEvents   []string
	}{
		{
			name:       "a failed pre-archive refuses archiving the agents",
			call:       byAgents,
			hookErrs:   map[string]error{"lock": errors.New("uncommitted work")},
			wantErr:    "uncommitted work",
			wantEvents: refused,
		},
		{
			name:       "a failed pre-archive refuses archiving the workflow",
			call:       byWorkflow,
			hookErrs:   map[string]error{"lock": errors.New("uncommitted work")},
			wantErr:    "uncommitted work",
			wantEvents: refused,
		},
		{
			name:         "archiving the agents passes pre-archive over a directory that has gone",
			call:         byAgents,
			hookErrs:     map[string]error{"lock": errors.New("uncommitted work")},
			removeDir:    true,
			wantArchived: true,
			wantEvents:   postOnly,
		},
		{
			name:         "archiving the workflow passes pre-archive over a directory that has gone",
			call:         byWorkflow,
			hookErrs:     map[string]error{"lock": errors.New("uncommitted work")},
			removeDir:    true,
			wantArchived: true,
			wantEvents:   postOnly,
		},
		{
			name:         "archiving the agents runs post-archive once it has",
			call:         byAgents,
			wantArchived: true,
			wantEvents:   archived,
		},
		{
			name:         "archiving the workflow runs post-archive once it has",
			call:         byWorkflow,
			wantArchived: true,
			wantEvents:   archived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()

			lifecycle := &fakeLifecycle{hookErrs: tt.hookErrs}
			workflowID, agents := seedHookedAgents(t, st, []workflowstore.Hook{
				{Event: workflowstore.HookPreArchive, Script: "lock"},
				{Event: workflowstore.HookPostArchive, Script: "give-back-the-slot"},
			}, "auth", "billing")
			if tt.removeDir {
				for _, agent := range agents {
					require.NoError(t, os.RemoveAll(agent.Cwd))
				}
			}

			err := tt.call.archive(t, st, lifecycle, workflowID, agents)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			for _, agent := range agents {
				after, err := st.Agents.GetByID(ctx, agent.ID)
				require.NoError(t, err)
				assert.Equal(t, tt.wantArchived, after.ArchivedAt != nil, "whether %s was archived", agent.Name)
			}

			require.Eventually(t, func() bool {
				return len(lifecycle.ranHooks()) >= len(tt.wantEvents)
			}, 5*time.Second, 10*time.Millisecond)
			require.Never(t, func() bool {
				return len(lifecycle.ranHooks()) > len(tt.wantEvents)
			}, 300*time.Millisecond, 20*time.Millisecond)

			assert.ElementsMatch(t, tt.wantEvents, ranEvents(lifecycle))
		})
	}
}

// TestPreRestore covers a veto on the way out of the archive and the directory that has gone.
func TestPreRestore(t *testing.T) {
	restoreAgent := func(t *testing.T, st *testStore, lifecycle *fakeLifecycle, _ string, agent agentstore.Agent) error {
		t.Helper()

		manager := st.agentServiceWithLifecycle(tmuxtest.NewFake(), harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
		_, err := manager.lifecycle.RestoreAgents(t.Context(), []string{agent.ID})

		return err
	}
	restoreWorkflow := func(t *testing.T, st *testStore, lifecycle *fakeLifecycle, workflowID string, _ agentstore.Agent) error {
		t.Helper()

		workflows := st.workflowService(workflowServiceOptions{tmux: tmuxtest.NewFake(), lifecycle: lifecycle})
		_, err := workflows.RestoreWorkflow(t.Context(), workflowID)

		return err
	}

	tests := []struct {
		name       string
		restore    func(t *testing.T, st *testStore, lifecycle *fakeLifecycle, workflowID string, agent agentstore.Agent) error
		removeDir  bool
		nameTaken  bool
		wantErr    string
		wantHooks  int
		wantActive bool
	}{
		{
			name:      "restoring the agent is refused by a failed hook",
			restore:   restoreAgent,
			wantErr:   "slot taken",
			wantHooks: 1,
		},
		{
			name:      "restoring the workflow is refused by a failed hook",
			restore:   restoreWorkflow,
			wantErr:   "slot taken",
			wantHooks: 1,
		},
		{
			name:      "restoring the agent runs no hook when its name is taken",
			restore:   restoreAgent,
			nameTaken: true,
			wantErr:   "rename whatever holds auth",
		},
		{
			name:      "restoring the workflow runs no hook when its name is taken",
			restore:   restoreWorkflow,
			nameTaken: true,
			wantErr:   "rename whatever holds auth",
		},
		{
			name:       "restoring the agent passes over a directory that has gone",
			restore:    restoreAgent,
			removeDir:  true,
			wantActive: true,
		},
		{
			name:       "restoring the workflow passes over a directory that has gone",
			restore:    restoreWorkflow,
			removeDir:  true,
			wantActive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()

			lifecycle := &fakeLifecycle{hookErrs: map[string]error{"check-in": errors.New("slot taken")}}
			workflowID, agents := seedHookedAgents(t, st, []workflowstore.Hook{
				{Event: workflowstore.HookPreRestore, Script: "check-in"},
			}, "auth")
			require.NoError(t, st.workflowService(workflowServiceOptions{tmux: tmuxtest.NewFake()}).ArchiveWorkflow(ctx, workflowID))
			if tt.removeDir {
				require.NoError(t, os.Remove(agents[0].Cwd))
			}
			if tt.nameTaken {
				seedInWorkflow(t, st, workflowID, "auth", "%9", agentstore.StateIdle)
			}

			err := tt.restore(t, st, lifecycle, workflowID, agents[0])
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Len(t, lifecycle.ranHooks(), tt.wantHooks)

			after, err := st.Agents.GetByID(ctx, agents[0].ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantActive, after.ArchivedAt == nil, "whether the agent came out of the archive")
		})
	}
}

// TestArchivingAnArchivedAgentRunsNoHooks covers a second archive of an agent already in the archive.
func TestArchivingAnArchivedAgentRunsNoHooks(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmuxtest.NewFake(), harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
	_, agents := seedHookedAgents(t, st, []workflowstore.Hook{
		{Event: workflowstore.HookPreArchive, Script: "lock"},
		{Event: workflowstore.HookPostArchive, Script: "give-back-the-slot"},
	}, "auth")

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, agentstore.IDs(agents)))
	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) == 2
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, os.RemoveAll(agents[0].Cwd))
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, agentstore.IDs(agents)))
	require.Never(t, func() bool {
		return len(lifecycle.ranHooks()) > 2
	}, 300*time.Millisecond, 20*time.Millisecond)
}

// TestDeleteFiresPreDeleteThenPostDelete covers the two moments either side of the directory going.
func TestDeleteFiresPreDeleteThenPostDelete(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	config := worktreeConfig(t, "backend")
	config.Hooks = []workflowstore.Hook{
		{Event: workflowstore.HookPreDelete, Script: "say-goodbye"},
		{Event: workflowstore.HookPostDelete, Script: "forget"},
	}
	created, err := st.Workflows.Create(ctx, config)
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	agent.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, agent))

	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{agent.ID}))

	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) == 2
	}, 5*time.Second, 10*time.Millisecond, "post-delete runs once the row is dropped")

	assert.Equal(t, []ranHook{
		{Event: workflowstore.HookPreDelete, Script: "say-goodbye", Cwd: agent.Cwd},
		{Event: workflowstore.HookPostDelete, Script: "forget", Cwd: agent.Cwd},
	}, lifecycle.ranHooks())

	remaining, err := st.Agents.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

// TestDeleteWaitsForTheHooksStillRunning covers the worktree outliving a hook from an earlier moment.
func TestDeleteWaitsForTheHooksStillRunning(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	running := make(chan struct{})
	lifecycle := &fakeLifecycle{holdHooks: running}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	config := worktreeConfig(t, "backend")
	config.Hooks = []workflowstore.Hook{{Event: workflowstore.HookPostArchive, Script: "give-back-the-slot"}}
	created, err := st.Workflows.Create(ctx, config)
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	agent.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, agent))

	startHooks(ctx, hookRunParams{
		scripts: lifecycle,
		agent:   agent,
		hooks:   config.Hooks,
		event:   workflowstore.HookPostArchive,
	})
	require.NoError(t, manager.lifecycle.DeleteAgents(ctx, []string{agent.ID}))

	present := func() bool {
		remaining, err := st.Agents.ListAll(ctx)

		return err == nil && len(remaining) == 1
	}

	require.Never(t, func() bool { return !present() }, 300*time.Millisecond, 20*time.Millisecond,
		"the row stays while a hook is running")

	close(running)

	require.Eventually(t, func() bool { return !present() }, 5*time.Second, 10*time.Millisecond,
		"and goes once it has finished")
}

// TestAStartedHookHoldsItsPlaceBeforeItsGoroutineIsScheduled covers the gap between a start and the goroutine running.
func TestAStartedHookHoldsItsPlaceBeforeItsGoroutineIsScheduled(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	hold := make(chan struct{})
	lifecycle := &fakeLifecycle{holdHooks: hold}
	_, agents := seedHookedAgents(t, st, []workflowstore.Hook{
		{Event: workflowstore.HookPostArchive, Script: "give-back-the-slot"},
	}, "auth")

	startHooksFor(ctx, batchHooksParams{
		scripts:   lifecycle,
		workflows: st.Workflows,
		agents:    agents,
		event:     workflowstore.HookPostArchive,
	})

	waiting, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	require.Error(t, lifecycle.AwaitHooks(waiting, agents[0].ID), "a wait straight after the start sees the hook")

	close(hold)
	require.NoError(t, lifecycle.AwaitHooks(ctx, agents[0].ID), "and ends once it has finished")
}
