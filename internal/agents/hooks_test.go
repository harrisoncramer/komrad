package agents

import (
	"errors"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgentRunsPreLaunchHooksInOrder covers the pair this table exists for: a worktree
// workflow that also wants something done inside the worktree before the CLI starts.
func TestNewAgentRunsPreLaunchHooksInOrder(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	provisioned := t.TempDir()
	lifecycle := &fakeLifecycle{dir: provisioned}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:            "backend",
		ProvisionAction: "make-worktree",
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Action: "install-deps"},
			{Event: workflowstore.HookPreLaunch, Action: "copy-env"},
			{Event: workflowstore.HookPostLaunch, Action: "announce"},
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
	assert.Equal(t, ranHook{Event: workflowstore.HookPreLaunch, Action: "install-deps", Cwd: provisioned}, ran[0],
		"the first pre-launch hook runs in the directory the provisioner named")
	assert.Equal(t, ranHook{Event: workflowstore.HookPreLaunch, Action: "copy-env", Cwd: provisioned}, ran[1],
		"and the second runs after it, in the order the workflow lists them")
}

// TestNewAgentRunsPostLaunchHooksInTheBackground pins the other half of the pair: the call
// does not wait for them, since the agent is already in its pane by then.
func TestNewAgentRunsPostLaunchHooksInTheBackground(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{dir: t.TempDir()}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:  "backend",
		Hooks: []workflowstore.Hook{{Event: workflowstore.HookPostLaunch, Action: "announce"}},
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
	assert.Equal(t, "announce", ran[0].Action)
}

// TestNewAgentAbandonsACreateAPreLaunchHookRefused pins a failing pre-launch hook taking the
// create with it, since there is no agent yet and a directory built for one that never
// started is a directory nothing will ever tear down.
func TestNewAgentAbandonsACreateAPreLaunchHookRefused(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{
		dir:      t.TempDir(),
		hookErrs: map[string]error{"install-deps": errors.New("mise refused")},
	}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:              "backend",
		ProvisionAction:   "make-worktree",
		DeprovisionAction: "drop-worktree",
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Action: "install-deps"},
			{Event: workflowstore.HookPreLaunch, Action: "copy-env"},
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
	assert.Equal(t, "install-deps", ran[0].Action)
	assert.Equal(t, []string{"drop-worktree"}, lifecycle.deprovision, "what the provisioner built is given back")

	agents, listErr := st.List(t.Context())
	require.NoError(t, listErr)
	require.Len(t, agents, 1)
	assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State)
}

// TestNewChildAgentRunsNoHooks pins a child running no moment of its own, since it is spawned
// into a directory its parent already prepared.
func TestNewChildAgentRunsNoHooks(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{dir: t.TempDir()}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:  "backend",
		Hooks: []workflowstore.Hook{{Event: workflowstore.HookPreLaunch, Action: "install-deps"}},
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

// TestNewWorkflowRefusesAHookOnAMomentWeaveDoesNotFire pins the refusal, since a hook hung on
// a moment nothing fires is a script that silently never runs.
func TestNewWorkflowRefusesAHookOnAMomentWeaveDoesNotFire(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())

	_, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name:  "backend",
		Hooks: []workflowstore.Hook{{Event: "pre-lunch", Action: "install-deps"}},
	})

	require.ErrorIs(t, err, ErrUnknownHookEvent)
}

// TestWorkflowHooksRoundTripInOrder pins the hooks coming back as they were written, since
// the order they are listed in is the order they run.
func TestWorkflowHooksRoundTripInOrder(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name: "backend",
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Action: "install-deps"},
			{Event: workflowstore.HookPreLaunch, Action: "copy-env"},
			{Event: workflowstore.HookPostLaunch, Action: "announce"},
			{Event: workflowstore.HookPreLaunch, Action: ""},
		},
	})
	require.NoError(t, err)

	got, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, []workflowstore.Hook{
		{Event: workflowstore.HookPreLaunch, Action: "install-deps"},
		{Event: workflowstore.HookPreLaunch, Action: "copy-env"},
		{Event: workflowstore.HookPostLaunch, Action: "announce"},
	}, got.Hooks, "the blank a None select sends is dropped rather than stored")

	require.NoError(t, workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:  "backend",
		Hooks: []workflowstore.Hook{{Event: workflowstore.HookPostLaunch, Action: "announce"}},
	}))

	updated, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, []workflowstore.Hook{{Event: workflowstore.HookPostLaunch, Action: "announce"}}, updated.Hooks,
		"an update writes the whole list rather than adding to it")
}
