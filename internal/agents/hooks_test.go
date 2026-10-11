package agents

import (
	"errors"
	"os"
	"testing"
	"time"

	"komrad/internal/scriptrun"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tmuxtest"
	"komrad/lib/xptr"

	"context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/harnesstest"
	"komrad/lib/xslice"
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
			{Event: workflowstore.HookAgentPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookAgentPreLaunch, Script: "copy-env"},
			{Event: workflowstore.HookAgentPostLaunch, Script: "announce"},
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
		Event:  workflowstore.HookAgentPreLaunch,
		Script: "install-deps",
		Cwd:    provisioned,
	}, ran[0],
		"the first agent.pre-launch hook runs in the directory the provisioner named")
	assert.Equal(t, ranHook{
		Event:  workflowstore.HookAgentPreLaunch,
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
		Hooks: []workflowstore.Hook{{Event: workflowstore.HookAgentPostLaunch, Script: "announce"}},
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
	}, 5*time.Second, 10*time.Millisecond, "the agent.post-launch hook runs once the pane is up")

	ran := lifecycle.ranHooks()
	assert.Equal(t, workflowstore.HookAgentPostLaunch, ran[0].Event)
	assert.Equal(t, "announce", ran[0].Script)
}

// TestNewAgentAbandonsACreateAPreLaunchHookRefused pins a failing agent.pre-launch hook taking the
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
			{Event: workflowstore.HookAgentPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookAgentPreLaunch, Script: "copy-env"},
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
	assert.Equal(t, []string{scriptrun.WorktreeDeprovisioner}, lifecycle.deprovision, "what the provisioner built is given back")

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
		Hooks: []workflowstore.Hook{{Event: workflowstore.HookAgentPreLaunch, Script: "install-deps"}},
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
			{Event: workflowstore.HookAgentPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookAgentPostLaunch, Script: "announce"},
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

// TestWorkflowRefusesAHookScriptTheHooksDirectoryDoesNotHold pins a misnamed hook being refused
// when it is saved.
func TestWorkflowRefusesAHookScriptTheHooksDirectoryDoesNotHold(t *testing.T) {
	st := newStore(t)
	workflows := st.workflowService(workflowServiceOptions{tmux: tmuxtest.NewFake(), hooks: knownHooks{"install-deps"}})

	_, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name:  "backend",
		Hooks: []workflowstore.Hook{{Event: workflowstore.HookAgentPreDelete, Script: "instal-deps"}},
	})
	require.ErrorIs(t, err, ErrUnknownHookScript)

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookAgentPreDelete, Script: "install-deps"}},
	})
	require.NoError(t, err)

	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookAgentPreDelete, Script: "instal-deps"}},
	})
	require.ErrorIs(t, err, ErrUnknownHookScript)

	stored, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, []workflowstore.Hook{{Event: workflowstore.HookAgentPreDelete, Script: "install-deps"}}, stored.Hooks,
		"the refused update leaves the hooks it had")
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
			{Event: workflowstore.HookAgentPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookAgentPreLaunch, Script: "copy-env"},
			{Event: workflowstore.HookAgentPostLaunch, Script: "announce"},
			{Event: workflowstore.HookAgentPreLaunch, Script: ""},
		},
	})
	require.NoError(t, err)

	got, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, []workflowstore.Hook{
		{Event: workflowstore.HookAgentPreLaunch, Script: "install-deps"},
		{Event: workflowstore.HookAgentPreLaunch, Script: "copy-env"},
		{Event: workflowstore.HookAgentPostLaunch, Script: "announce"},
	}, got.Hooks, "the blank a None select sends is dropped rather than stored")

	require.NoError(t, workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookAgentPostLaunch, Script: "announce"}},
	}))

	updated, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, []workflowstore.Hook{{Event: workflowstore.HookAgentPostLaunch, Script: "announce"}}, updated.Hooks,
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
			{Event: workflowstore.HookAgentPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookAgentPostResume, Script: "announce-back"},
		},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	agent.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, agent))

	_, err = manager.resume.ResumeAgent(ctx, ResumeAgentParams{ID: agent.ID})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) == 1
	}, 5*time.Second, 10*time.Millisecond, "the agent.post-resume hook runs once the pane is back")

	ran := lifecycle.ranHooks()
	assert.Equal(t, workflowstore.HookAgentPostResume, ran[0].Event)
	assert.Equal(t, "announce-back", ran[0].Script, "and agent.pre-launch does not rerun")
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
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookAgentPostRestore, Script: "unlock"}},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "auth"))

	_, err = manager.lifecycle.RestoreAgents(ctx, []string{agent.ID})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) == 1
	}, 5*time.Second, 10*time.Millisecond, "the agent.post-restore hook runs once the row is back")

	assert.Equal(t, workflowstore.HookAgentPostRestore, lifecycle.ranHooks()[0].Event)
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

				_, err := manager.resume.ResumeAgent(t.Context(), ResumeAgentParams{ID: agent.ID})
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
					{Event: workflowstore.HookAgentPostResume, Script: "announce-back"},
					{Event: workflowstore.HookAgentPostRestore, Script: "unlock"},
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
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookAgentPostRestore, Script: "unlock"}},
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
	}, 5*time.Second, 10*time.Millisecond, "the archived agent's agent.post-restore hook runs")

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
		Hooks:    []workflowstore.Hook{{Event: workflowstore.HookAgentPostRestore, Script: "unlock"}},
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
	}, 5*time.Second, 10*time.Millisecond, "the agent.post-restore hook runs for the agent inside")

	assert.Equal(t, []ranHook{{
		Event:  workflowstore.HookAgentPostRestore,
		Script: "unlock",
		Cwd:    agent.Cwd,
	}}, lifecycle.ranHooks())
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
			{Event: workflowstore.HookAgentPreResume, Script: "warm-up"},
			{Event: workflowstore.HookAgentPostResume, Script: "announce-back"},
		},
	})
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	agent.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, agent))

	_, err = manager.resume.ResumeAgent(ctx, ResumeAgentParams{ID: agent.ID})
	require.ErrorContains(t, err, "no slot free")
	require.ErrorContains(t, err, `"auth"`, "the refusal names the agent")
	assert.Zero(t, tmux.WindowsOpened(), "no pane was opened")

	require.Never(t, func() bool {
		return len(lifecycle.ranHooks()) > 1
	}, 300*time.Millisecond, 20*time.Millisecond, "agent.post-resume never follows a refused resume")

	assert.Equal(t, []ranHook{{
		Event:  workflowstore.HookAgentPreResume,
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

				return manager.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: agentstore.IDs(agents)})
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
		workflowstore.HookAgentPreArchive,
		workflowstore.HookAgentPreArchive,
	}
	archived := []string{
		workflowstore.HookAgentPreArchive,
		workflowstore.HookAgentPreArchive,
		workflowstore.HookAgentPostArchive,
		workflowstore.HookAgentPostArchive,
	}
	postOnly := []string{
		workflowstore.HookAgentPostArchive,
		workflowstore.HookAgentPostArchive,
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
			name:       "a failed agent.pre-archive refuses archiving the agents",
			call:       byAgents,
			hookErrs:   map[string]error{"lock": errors.New("uncommitted work")},
			wantErr:    "uncommitted work",
			wantEvents: refused,
		},
		{
			name:       "a failed agent.pre-archive refuses archiving the workflow",
			call:       byWorkflow,
			hookErrs:   map[string]error{"lock": errors.New("uncommitted work")},
			wantErr:    "uncommitted work",
			wantEvents: refused,
		},
		{
			name:         "archiving the agents passes agent.pre-archive over a directory that has gone",
			call:         byAgents,
			hookErrs:     map[string]error{"lock": errors.New("uncommitted work")},
			removeDir:    true,
			wantArchived: true,
			wantEvents:   postOnly,
		},
		{
			name:         "archiving the workflow passes agent.pre-archive over a directory that has gone",
			call:         byWorkflow,
			hookErrs:     map[string]error{"lock": errors.New("uncommitted work")},
			removeDir:    true,
			wantArchived: true,
			wantEvents:   postOnly,
		},
		{
			name:         "archiving the agents runs agent.post-archive once it has",
			call:         byAgents,
			wantArchived: true,
			wantEvents:   archived,
		},
		{
			name:         "archiving the workflow runs agent.post-archive once it has",
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
				{Event: workflowstore.HookAgentPreArchive, Script: "lock"},
				{Event: workflowstore.HookAgentPostArchive, Script: "give-back-the-slot"},
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
				{Event: workflowstore.HookAgentPreRestore, Script: "check-in"},
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
		{Event: workflowstore.HookAgentPreArchive, Script: "lock"},
		{Event: workflowstore.HookAgentPostArchive, Script: "give-back-the-slot"},
	}, "auth")

	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, ArchiveAgentsParams{IDs: agentstore.IDs(agents)}))
	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) == 2
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, os.RemoveAll(agents[0].Cwd))
	require.NoError(t, manager.lifecycle.ArchiveAgents(ctx, ArchiveAgentsParams{IDs: agentstore.IDs(agents)}))
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
		{Event: workflowstore.HookAgentPreDelete, Script: "say-goodbye"},
		{Event: workflowstore.HookAgentPostDelete, Script: "forget"},
	}
	created, err := st.Workflows.Create(ctx, config)
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	agent.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, agent))

	require.NoError(t, manager.lifecycle.deleteNow(ctx, []string{agent.ID}))

	require.Eventually(t, func() bool {
		return len(lifecycle.ranHooks()) == 2
	}, 5*time.Second, 10*time.Millisecond, "agent.post-delete runs once the row is dropped")

	assert.Equal(t, []ranHook{
		{
			Event:  workflowstore.HookAgentPreDelete,
			Script: "say-goodbye",
			Cwd:    agent.Cwd,
		},
		{
			Event:  workflowstore.HookAgentPostDelete,
			Script: "forget",
			Cwd:    agent.Cwd,
		},
	}, lifecycle.ranHooks())

	remaining, err := st.Agents.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

// TestDeleteWaitsForTheHooksStillRunning covers the row outliving a hook from an earlier moment.
func TestDeleteWaitsForTheHooksStillRunning(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	ctx := t.Context()

	running := make(chan struct{})
	lifecycle := &fakeLifecycle{holdHooks: running}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	config := worktreeConfig(t, "backend")
	config.Hooks = []workflowstore.Hook{{Event: workflowstore.HookAgentPostArchive, Script: "give-back-the-slot"}}
	created, err := st.Workflows.Create(ctx, config)
	require.NoError(t, err)

	agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
	agent.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, agent))

	scriptrun.HookRun{
		Scripts: lifecycle,
		Agent:   agent,
		Hooks:   config.Hooks,
		Event:   workflowstore.HookAgentPostArchive,
	}.Start(ctx)
	deleted := make(chan error, 1)
	go func() { deleted <- manager.lifecycle.deleteNow(ctx, []string{agent.ID}) }()

	present := func() bool {
		remaining, err := st.Agents.ListAll(ctx)

		return err == nil && len(remaining) == 1
	}

	require.Never(t, func() bool { return !present() }, 300*time.Millisecond, 20*time.Millisecond,
		"the row stays while a hook is running")

	close(running)

	require.NoError(t, <-deleted)
	assert.False(t, present(), "and goes once it has finished")
}

// TestAStartedHookHoldsItsPlaceBeforeItsGoroutineIsScheduled covers the gap between a start and the goroutine running.
func TestAStartedHookHoldsItsPlaceBeforeItsGoroutineIsScheduled(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	hold := make(chan struct{})
	lifecycle := &fakeLifecycle{holdHooks: hold}
	_, agents := seedHookedAgents(t, st, []workflowstore.Hook{
		{Event: workflowstore.HookAgentPostArchive, Script: "give-back-the-slot"},
	}, "auth")

	startHooksFor(ctx, batchHooksParams{
		scripts:   lifecycle,
		workflows: st.Workflows,
		agents:    agents,
		event:     workflowstore.HookAgentPostArchive,
	})

	waiting, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	require.Error(t, lifecycle.AwaitHooks(waiting, agents[0].ID), "a wait straight after the start sees the hook")

	close(hold)
	require.NoError(t, lifecycle.AwaitHooks(ctx, agents[0].ID), "and ends once it has finished")
}

const taskHook = "task-hook"

// taskHookConfig is a workflow hanging taskHook on one of a task's moments, with a worktree or without.
func taskHookConfig(t *testing.T, event string, worktree bool) workflowstore.Config {
	t.Helper()

	config := workflowstore.Config{
		Name:  "backend",
		Cwd:   fakeCheckout(t),
		Hooks: []workflowstore.Hook{{Event: event, Script: taskHook}},
	}
	if worktree {
		config.Worktree = workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"}
	}

	return config
}

func launchOnTask(t *testing.T, fx *taskDeleteFixture, params NewAgentParams) (*agentstore.Agent, error) {
	t.Helper()

	params.WorkflowID = fx.workflow.ID
	if params.ParentAgentID == "" {
		params.Dir = t.TempDir()
	}

	return fx.agents.launch.NewAgent(t.Context(), params)
}

func waitForTaskHooks(t *testing.T, lifecycle *fakeLifecycle, want int) []agentstore.Agent {
	t.Helper()

	require.Eventually(t, func() bool { return len(lifecycle.ranTaskHooks()) >= want }, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return len(lifecycle.ranTaskHooks()) > want }, 300*time.Millisecond, 20*time.Millisecond)

	return lifecycle.ranTaskHooks()
}

// TestTaskPreOpen covers task.pre-open on a create, in the task's worktree when it has one.
func TestTaskPreOpen(t *testing.T) {
	tests := []struct {
		name           string
		worktree       bool
		hookErr        error
		join           bool
		wantErr        bool
		wantOpens      int
		wantInWorktree bool
	}{
		{
			name:      "a failed pre-open abandons the create",
			worktree:  true,
			hookErr:   errors.New("cache is cold"),
			wantErr:   true,
			wantOpens: 1,
		},
		{
			name:           "a reviewer joining its parent's task does not open it",
			worktree:       true,
			join:           true,
			wantOpens:      1,
			wantInWorktree: true,
		},
		{
			name:      "a task with no worktree opens in the workflow's directory",
			wantOpens: 1,
		},
		{
			name:           "a task with a worktree opens in it",
			worktree:       true,
			wantOpens:      1,
			wantInWorktree: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provisioned := t.TempDir()
			lifecycle := &fakeLifecycle{
				dir:      provisioned,
				hookErrs: map[string]error{taskHook: tt.hookErr},
			}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, tt.worktree))

			agent, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			if tt.join {
				require.NoError(t, err)
				_, err = launchOnTask(t, fx, childOf(*agent, "reviewer"))
			}
			if tt.wantErr {
				require.ErrorContains(t, err, "cache is cold")
				listed, listErr := fx.st.Agents.List(t.Context())
				require.NoError(t, listErr)
				require.Len(t, listed, 1)
				assert.Equal(t, agentstore.StateFailedToCreate, listed[0].State)
				assert.Equal(t, []string{scriptrun.WorktreeDeprovisioner}, lifecycle.ranDeprovisions(), "what setup built is given back")
			} else {
				require.NoError(t, err)
			}

			ran := lifecycle.ranTaskHooks()
			require.Len(t, ran, tt.wantOpens)
			opened := ran[0]
			if tt.wantInWorktree {
				assert.Equal(t, provisioned, opened.Cwd)
				assert.Nil(t, opened.ProvisionEnabled, "the worktree's variables reach the hook")
			} else if !tt.wantErr {
				assert.Equal(t, fx.workflow.Cwd, opened.Cwd)
				assert.Equal(t, xptr.Ptr(false), opened.ProvisionEnabled, "no worktree's variables reach the hook")
			}
			if agent != nil {
				assert.Equal(t, agent.ID, opened.ID, "the hook runs as the agent that opened the task")
			}
		})
	}
}

// TestRetryOpensTheTaskAFailedPreOpenLeft pins that an agent which failed to create holds no task open.
func TestRetryOpensTheTaskAFailedPreOpenLeft(t *testing.T) {
	lifecycle := &fakeLifecycle{
		dir:      t.TempDir(),
		hookErrs: map[string]error{taskHook: errors.New("cache is cold")},
	}
	fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, true))
	_, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
	require.Error(t, err)
	failed, err := fx.st.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, failed, 1)

	lifecycle.hookErrs = nil
	_, err = fx.agents.launch.RetryCreate(t.Context(), RetryCreateParams{ID: failed[0].ID})
	require.NoError(t, err)

	ran := lifecycle.ranTaskHooks()
	require.Len(t, ran, 2, "the retry opens the task again")
	assert.Equal(t, failed[0].ID, ran[1].ID)
}

// TestJoinOpensTheTaskItRestores covers a writer joining an archived task's checkout running task.pre-open there.
func TestJoinOpensTheTaskItRestores(t *testing.T) {
	tests := []struct {
		name         string
		hookErr      error
		wantArchived bool
	}{
		{
			name:         "a failed pre-open archives the task again",
			hookErr:      errors.New("cache is cold"),
			wantArchived: true,
		},
		{
			name: "a join that restores the task opens it in the worktree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			provisioned := t.TempDir()
			lifecycle := &fakeLifecycle{dir: provisioned}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, true))
			first, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			require.NoError(t, err)
			require.NoError(t, fx.agents.lifecycle.ArchiveAgents(ctx, ArchiveAgentsParams{IDs: []string{first.ID}}))
			lifecycle.hookErrs = map[string]error{taskHook: tt.hookErr}

			next, err := fx.agents.launch.NewAgent(ctx, NewAgentParams{
				Title:      "next",
				Prompt:     "carry on",
				WorkflowID: fx.workflow.ID,
				Task:       *first.TaskID,
			})

			task, getErr := fx.st.Tasks.Get(ctx, *first.TaskID)
			require.NoError(t, getErr)
			ran := lifecycle.ranTaskHooks()
			require.Len(t, ran, 2, "the join opens the task again")
			assert.Equal(t, provisioned, ran[1].Cwd)
			if tt.wantArchived {
				require.ErrorContains(t, err, "cache is cold")
				assert.NotNil(t, task.ArchivedAt)

				return
			}
			require.NoError(t, err)
			assert.Nil(t, task.ArchivedAt)
			assert.Equal(t, next.ID, ran[1].ID, "the hook runs as the joining writer")
		})
	}
}

// TestTaskPostArchive covers task.post-archive firing once a task's last live agent is archived.
func TestTaskPostArchive(t *testing.T) {
	tests := []struct {
		name        string
		worktree    bool
		archive     func(t *testing.T, fx *taskDeleteFixture, lifecycle *fakeLifecycle, ids []string) error
		wantCwd     func(fx *taskDeleteFixture, writer agentstore.Agent) string
		wantNoneRun bool
	}{
		{
			name:     "archiving the task's reviewer leaves it open",
			worktree: true,
			archive: func(t *testing.T, fx *taskDeleteFixture, _ *fakeLifecycle, ids []string) error {
				t.Helper()

				return fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: ids[1:]})
			},
			wantNoneRun: true,
		},
		{
			name: "a task with no worktree runs agent.post-archive in the workflow's directory",
			archive: func(t *testing.T, fx *taskDeleteFixture, _ *fakeLifecycle, ids []string) error {
				t.Helper()

				return fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: ids})
			},
			wantCwd: func(fx *taskDeleteFixture, _ agentstore.Agent) string { return fx.workflow.Cwd },
		},
		{
			name:     "archiving the last agents runs agent.post-archive once, in the worktree",
			worktree: true,
			archive: func(t *testing.T, fx *taskDeleteFixture, _ *fakeLifecycle, ids []string) error {
				t.Helper()

				return fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: ids})
			},
			wantCwd: func(_ *taskDeleteFixture, writer agentstore.Agent) string { return writer.Cwd },
		},
		{
			name:     "archiving the workflow runs agent.post-archive",
			worktree: true,
			archive: func(t *testing.T, fx *taskDeleteFixture, lifecycle *fakeLifecycle, _ []string) error {
				t.Helper()

				return fx.st.workflowService(workflowServiceOptions{tmux: tmuxtest.NewFake(), lifecycle: lifecycle}).ArchiveWorkflow(t.Context(), fx.workflow.ID)
			},
			wantCwd: func(_ *taskDeleteFixture, writer agentstore.Agent) string { return writer.Cwd },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{dir: t.TempDir()}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPostArchive, tt.worktree))
			writer, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			require.NoError(t, err)
			reviewer, err := launchOnTask(t, fx, childOf(*writer, "reviewer"))
			require.NoError(t, err)

			require.NoError(t, tt.archive(t, fx, lifecycle, []string{writer.ID, reviewer.ID}))

			if tt.wantNoneRun {
				waitForTaskHooks(t, lifecycle, 0)
				return
			}
			ran := waitForTaskHooks(t, lifecycle, 1)
			assert.Equal(t, writer.ID, ran[0].ID, "the hook runs as the task's writer rather than its reviewer")
			assert.Equal(t, tt.wantCwd(fx, *writer), ran[0].Cwd)
		})
	}
}

// TestRestoreOpensTheTask pins that a restore runs task.pre-open before it brings an archived task back.
func TestRestoreOpensTheTask(t *testing.T) {
	tests := []struct {
		name         string
		hookErr      error
		wantRestored bool
	}{
		{
			name:    "a failed pre-open refuses the restore",
			hookErr: errors.New("cache is cold"),
		},
		{
			name:         "restoring the task's agent opens it",
			wantRestored: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{dir: t.TempDir()}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, true))
			agent, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			require.NoError(t, err)
			require.NoError(t, fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: []string{agent.ID}}))

			lifecycle.hookErrs = map[string]error{taskHook: tt.hookErr}
			_, err = fx.agents.lifecycle.RestoreAgents(t.Context(), []string{agent.ID})
			if tt.wantRestored {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "cache is cold")
			}

			assert.Equal(t, !tt.wantRestored, agentByID(t, fx.st, agent.ID).ArchivedAt != nil)
			assert.Equal(t, !tt.wantRestored, isTaskArchived(t, fx.st, *agent.TaskID))
			ran := lifecycle.ranTaskHooks()
			require.Len(t, ran, 2, "the create and the restore each open the task")
			assert.Equal(t, agent.ID, ran[1].ID)
			assert.Equal(t, agent.Cwd, ran[1].Cwd)
		})
	}
}

// TestTaskPreOpenWaitsForTheTasksPostArchive pins a task's hooks running one at a time.
func TestTaskPreOpenWaitsForTheTasksPostArchive(t *testing.T) {
	lifecycle := &fakeLifecycle{dir: t.TempDir()}
	config := taskHookConfig(t, workflowstore.HookTaskPreOpen, true)
	config.Hooks = append(config.Hooks, workflowstore.Hook{Event: workflowstore.HookTaskPostArchive, Script: "shrink"})
	fx := newTaskDeleteFixture(t, lifecycle, config)
	agent, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
	require.NoError(t, err)

	hold := make(chan struct{})
	lifecycle.holdHooks = hold
	require.NoError(t, fx.agents.lifecycle.ArchiveAgents(t.Context(), ArchiveAgentsParams{IDs: []string{agent.ID}}))

	restored := make(chan error, 1)
	go func() {
		_, err := fx.agents.lifecycle.RestoreAgents(t.Context(), []string{agent.ID})
		restored <- err
	}()
	require.Never(t, func() bool { return len(restored) > 0 }, 200*time.Millisecond, 20*time.Millisecond)

	close(hold)
	require.NoError(t, <-restored)
	assert.Equal(t, []string{
		workflowstore.HookTaskPreOpen,
		workflowstore.HookTaskPostArchive,
		workflowstore.HookTaskPreOpen,
	}, ranEvents(lifecycle))
}

// TestTaskPreDelete covers task.pre-delete running once no agent is left on a task, before its worktree goes.
func TestTaskPreDelete(t *testing.T) {
	tests := []struct {
		name            string
		worktree        bool
		hookErr         error
		deleteAgentOnly bool
		removeWorktree  bool
		wantDeleted     bool
		wantRan         bool
		wantInWorktree  bool
	}{
		{
			name:           "a failed agent.pre-delete keeps the task and its worktree",
			worktree:       true,
			hookErr:        errors.New("unpushed work"),
			wantRan:        true,
			wantInWorktree: true,
		},
		{
			name:            "deleting the agent of a task it leaves behind runs no agent.pre-delete",
			worktree:        true,
			deleteAgentOnly: true,
		},
		{
			name:           "a task whose worktree has gone runs agent.pre-delete in the workflow's directory",
			worktree:       true,
			removeWorktree: true,
			wantDeleted:    true,
			wantRan:        true,
		},
		{
			name:        "a task with no worktree runs agent.pre-delete in the workflow's directory",
			wantDeleted: true,
			wantRan:     true,
		},
		{
			name:           "a task with a worktree runs agent.pre-delete in it before giving it back",
			worktree:       true,
			wantDeleted:    true,
			wantRan:        true,
			wantInWorktree: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{
				dir:      t.TempDir(),
				hookErrs: map[string]error{taskHook: tt.hookErr},
			}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreDelete, tt.worktree))
			agent, err := launchOnTask(t, fx, NewAgentParams{Title: "auth"})
			require.NoError(t, err)

			if tt.removeWorktree {
				require.NoError(t, os.RemoveAll(agent.Cwd))
			}
			if tt.deleteAgentOnly {
				require.NoError(t, fx.agents.lifecycle.deleteNow(t.Context(), []string{agent.ID}))
			} else {
				require.NoError(t, fx.agents.lifecycle.DeleteTasks(t.Context(), []string{*agent.TaskID}))
			}

			kept, err := fx.st.Tasks.Get(t.Context(), *agent.TaskID)
			if tt.wantDeleted {
				require.ErrorIs(t, err, store.ErrNotFound)
			} else {
				require.NoError(t, err)
			}
			if tt.hookErr != nil {
				assert.Contains(t, kept.FailureReason, "unpushed work")
				assert.Empty(t, lifecycle.ranTaskDeprovisions(), "the worktree stays")
			}

			ran := lifecycle.ranTaskHooks()
			if !tt.wantRan {
				assert.Empty(t, ran)
				return
			}
			require.Len(t, ran, 1)
			assert.Empty(t, ran[0].ID, "no agent is left to run it as")
			if tt.wantInWorktree {
				assert.Equal(t, agent.Cwd, ran[0].Cwd)
				assert.Nil(t, ran[0].ProvisionEnabled)
			} else {
				assert.Equal(t, fx.workflow.Cwd, ran[0].Cwd)
				assert.Equal(t, xptr.Ptr(false), ran[0].ProvisionEnabled)
			}
		})
	}
}

// TestJoiningAgentWaitsForTheTaskToOpen pins that a joiner waits out the opener's task.pre-open and opens the task itself when that failed.
func TestJoiningAgentWaitsForTheTaskToOpen(t *testing.T) {
	tests := []struct {
		name        string
		hookErr     error
		wantOpeners int
	}{
		{
			name:        "a joiner opens the task itself once the agent opening it fails",
			hookErr:     errors.New("cache is cold"),
			wantOpeners: 2,
		},
		{
			name:        "a joiner starts once the task has opened, without opening it again",
			wantOpeners: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hold := make(chan struct{})
			lifecycle := &fakeLifecycle{
				dir:          t.TempDir(),
				holdHooks:    hold,
				hookErrsOnce: map[string]error{taskHook: tt.hookErr},
			}
			fx := newTaskDeleteFixture(t, lifecycle, taskHookConfig(t, workflowstore.HookTaskPreOpen, false))

			opened := make(chan error, 1)
			go func() {
				_, err := launchOnTask(t, fx, NewAgentParams{
					Title:    "auth",
					TaskSlug: "work",
				})
				opened <- err
			}()
			require.Eventually(t, func() bool {
				_, err := fx.st.Tasks.GetBySlug(t.Context(), "work")
				return err == nil
			}, 5*time.Second, 10*time.Millisecond)

			joined := make(chan error, 1)
			var joiner *agentstore.Agent
			go func() {
				agent, err := launchOnTask(t, fx, NewAgentParams{
					Title: "follow-up",
					Task:  "work",
				})
				joiner = agent
				joined <- err
			}()
			require.Never(t, func() bool { return len(joined) > 0 }, 200*time.Millisecond, 20*time.Millisecond)

			close(hold)
			openErr := <-opened
			require.NoError(t, <-joined)
			assert.Equal(t, tt.hookErr != nil, openErr != nil)

			ran := lifecycle.ranTaskHooks()
			require.Len(t, ran, tt.wantOpeners)
			if tt.hookErr != nil {
				assert.Equal(t, joiner.ID, ran[1].ID)
			}
		})
	}
}
