package agents

import (
	"errors"
	"testing"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryCreateRelaunches covers a failed launch being put back into a pane.
func TestRetryCreateRelaunches(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("bad flags")
	st := newStore(t)
	manager := newManager(tmux, st, "")

	params := NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		Flags:      "--bad-flag",
		Prompt:     "do the thing",
		WorkflowID: seedWorkflow(t, st),
	}

	_, err := manager.launch.NewAgent(t.Context(), params)
	require.Error(t, err)

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State)

	tmux.respawnErr = nil

	retried, err := manager.launch.RetryCreate(t.Context(), RetryCreateParams{
		ID:    agents[0].ID,
		Flags: "--good-flag",
	})
	require.NoError(t, err)
	require.NotNil(t, retried)

	got, err := st.Agents.GetByID(t.Context(), retried.ID)
	require.NoError(t, err)
	assert.Equal(t, agents[0].ID, got.ID, "the same row is reused")
	assert.Equal(t, "--good-flag", got.Flags, "flags were replaced")
	assert.NotEmpty(t, tmux.respawned, "the agent was launched into a pane")
}

// TestRetryCreateRefusesNonFailedAgent covers an agent that never failed to create.
func TestRetryCreateRefusesNonFailedAgent(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	params := NewAgentParams{
		Title:      "healthy",
		Dir:        t.TempDir(),
		Prompt:     "do the thing",
		WorkflowID: seedWorkflow(t, st),
	}

	created, err := manager.launch.NewAgent(t.Context(), params)
	require.NoError(t, err)

	_, err = manager.launch.RetryCreate(t.Context(), RetryCreateParams{ID: created.ID})
	require.ErrorIs(t, err, ErrAgentNotFailed)
}

// TestRetryCreateKeepsOriginalFlagsWhenNoneGiven covers a retry that names no flags of its own.
func TestRetryCreateKeepsOriginalFlagsWhenNoneGiven(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("boom")
	st := newStore(t)
	manager := newManager(tmux, st, "")

	params := NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		Flags:      "--original",
		Prompt:     "do the thing",
		WorkflowID: seedWorkflow(t, st),
	}

	_, err := manager.launch.NewAgent(t.Context(), params)
	require.Error(t, err)

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)

	tmux.respawnErr = nil

	retried, err := manager.launch.RetryCreate(t.Context(), RetryCreateParams{
		ID: agents[0].ID,
	})
	require.NoError(t, err)

	got, err := st.Agents.GetByID(t.Context(), retried.ID)
	require.NoError(t, err)
	assert.Equal(t, "--original", got.Flags, "original flags are preserved when no replacement given")
}

// TestRetryCreateFailsAgainOnBadLaunch covers a retry whose launch fails the same way.
func TestRetryCreateFailsAgainOnBadLaunch(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("still broken")
	st := newStore(t)
	manager := newManager(tmux, st, "")

	params := NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		Prompt:     "do the thing",
		WorkflowID: seedWorkflow(t, st),
	}

	_, err := manager.launch.NewAgent(t.Context(), params)
	require.Error(t, err)

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	id := agents[0].ID

	_, err = manager.launch.RetryCreate(t.Context(), RetryCreateParams{ID: id})
	require.Error(t, err)

	got, err := st.Agents.GetByID(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, agentstore.StateFailedToCreate, got.State, "goes back to failed_to_create for another retry")
}

// TestRetryCreateOntoAnotherHarness covers a retry that corrects the CLI the agent runs.
func TestRetryCreateOntoAnotherHarness(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("claude is not installed")
	st := newStore(t)

	launch := NewLaunchService(NewLaunchServiceParams{
		Tmux:               tmux,
		Agents:             st.Agents,
		Reviews:            reviewstore.New(st.Pool.GetDB()),
		Workflows:          st.Workflows,
		Tx:                 st.Pool,
		Launcher:           NewLauncher(NewLauncherParams{Tmux: tmux}),
		Harnesses:          fakeHarnesses{harness: &fakeHarness{}},
		UserDefaultHarness: testHarnessID,
		UserDefaultFlags: map[string]string{
			testHarnessID: "--dangerously-skip-permissions",
			"codex":       "--sandbox danger-full-access",
		},
	})

	_, err := launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		Prompt:     "do the thing",
		WorkflowID: seedWorkflow(t, st),
	})
	require.Error(t, err)

	failed, err := st.Get(t.Context(), "doomed")
	require.NoError(t, err)
	require.Equal(t, testHarnessID, failed.Harness)
	require.Equal(t, "--dangerously-skip-permissions", failed.Flags)

	tmux.respawnErr = nil

	retried, err := launch.RetryCreate(t.Context(), RetryCreateParams{
		ID:      failed.ID,
		Harness: "codex",
	})
	require.NoError(t, err)
	assert.Equal(t, "codex", retried.Harness, "the corrected CLI is handed back")

	got, err := st.Agents.GetByID(t.Context(), failed.ID)
	require.NoError(t, err)
	assert.Equal(t, "codex", got.Harness, "the row names the CLI that is running")
	assert.Equal(t, "--sandbox danger-full-access", got.Flags, "and the flags configured for it")
}

// TestRetryCreateOntoAnotherHarnessDropsWorkflowFlags covers a retry off the CLI a workflow's
// flags were written for.
func TestRetryCreateOntoAnotherHarnessDropsWorkflowFlags(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("boom")
	st := newStore(t)

	launch := NewLaunchService(NewLaunchServiceParams{
		Tmux:               tmux,
		Agents:             st.Agents,
		Reviews:            reviewstore.New(st.Pool.GetDB()),
		Workflows:          st.Workflows,
		Tx:                 st.Pool,
		Launcher:           NewLauncher(NewLauncherParams{Tmux: tmux}),
		Harnesses:          fakeHarnesses{harness: &fakeHarness{}},
		UserDefaultHarness: testHarnessID,
	})

	wf, err := st.Workflows.Create(t.Context(), workflowstore.Config{
		Name:    "workflow-" + uuid.NewString(),
		Harness: testHarnessID,
		Flags:   "--model opus",
	})
	require.NoError(t, err)

	_, err = launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		WorkflowID: wf.ID,
	})
	require.Error(t, err)

	failed, err := st.Get(t.Context(), "doomed")
	require.NoError(t, err)
	require.Equal(t, "--model opus", failed.Flags)

	tmux.respawnErr = nil

	_, err = launch.RetryCreate(t.Context(), RetryCreateParams{
		ID:      failed.ID,
		Harness: "codex",
	})
	require.NoError(t, err)

	got, err := st.Agents.GetByID(t.Context(), failed.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Flags, "a workflow's flags are written for the CLI it names")
}

// TestRetryCreateOntoAnotherHarnessRemintsTheSession covers the conversation an agent carries
// over a retry that changes the CLI.
func TestRetryCreateOntoAnotherHarnessRemintsTheSession(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("boom")
	st := newStore(t)

	launch := NewLaunchService(NewLaunchServiceParams{
		Tmux:               tmux,
		Agents:             st.Agents,
		Reviews:            reviewstore.New(st.Pool.GetDB()),
		Workflows:          st.Workflows,
		Tx:                 st.Pool,
		Launcher:           NewLauncher(NewLauncherParams{Tmux: tmux}),
		Harnesses:          fakeHarnesses{harness: &fakeHarness{}, byID: map[string]*fakeHarness{"codex": {id: "codex", noSession: true}}},
		UserDefaultHarness: testHarnessID,
	})

	_, err := launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		WorkflowID: seedWorkflow(t, st),
	})
	require.Error(t, err)

	failed, err := st.Get(t.Context(), "doomed")
	require.NoError(t, err)
	require.NotEmpty(t, failed.SessionID, "the CLI it failed on minted one")

	tmux.respawnErr = nil

	_, err = launch.RetryCreate(t.Context(), RetryCreateParams{
		ID:      failed.ID,
		Harness: "codex",
	})
	require.NoError(t, err)

	got, err := st.Agents.GetByID(t.Context(), failed.ID)
	require.NoError(t, err)
	assert.Empty(t, got.SessionID, "the conversation the old CLI minted is not carried over")
}

// TestRetryCreateKeepsTheSessionWhenTheHarnessDoesNot covers a retry that leaves the CLI alone.
func TestRetryCreateKeepsTheSessionWhenTheHarnessDoesNot(t *testing.T) {
	tmux := newFakeTmux()
	tmux.respawnErr = errors.New("boom")
	st := newStore(t)
	manager := newManager(tmux, st, "")

	_, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		WorkflowID: seedWorkflow(t, st),
	})
	require.Error(t, err)

	failed, err := st.Get(t.Context(), "doomed")
	require.NoError(t, err)
	require.NotEmpty(t, failed.SessionID)

	tmux.respawnErr = nil

	_, err = manager.launch.RetryCreate(t.Context(), RetryCreateParams{
		ID:      failed.ID,
		Harness: testHarnessID,
	})
	require.NoError(t, err)

	got, err := st.Agents.GetByID(t.Context(), failed.ID)
	require.NoError(t, err)
	assert.Equal(t, failed.SessionID, got.SessionID, "naming the CLI it already runs changes nothing")
}
