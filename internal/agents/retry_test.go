package agents

import (
	"errors"
	"testing"

	"weave/internal/harness"
	"weave/internal/harnesstest"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/tmuxtest"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryCreateRelaunches covers a failed launch being put back into a pane.
func TestRetryCreateRelaunches(t *testing.T) {
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("bad flags")
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

	tmux.RespawnErr = nil

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
	assert.NotEmpty(t, tmux.Respawned, "the agent was launched into a pane")
}

// TestRetryCreateRefusesNonFailedAgent covers an agent that never failed to create.
func TestRetryCreateRefusesNonFailedAgent(t *testing.T) {
	tmux := tmuxtest.NewFake()
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
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("boom")
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

	tmux.RespawnErr = nil

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
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("still broken")
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
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("claude is not installed")
	st := newStore(t)

	launch := NewLaunchService(NewLaunchServiceParams{
		Tmux:               tmux,
		Agents:             st.Agents,
		Reviews:            reviewstore.NewReviewStore(st.Pool.GetDB()),
		Workflows:          st.Workflows,
		Tx:                 st.Pool,
		Launcher:           NewLauncher(NewLauncherParams{Tmux: tmux}),
		Harnesses:          harnesstest.Registry{Harness: &harnesstest.Fake{}},
		UserDefaultHarness: harnesstest.FakeID,
		UserDefaultFlags: map[string]string{
			harnesstest.FakeID: "--dangerously-skip-permissions",
			"codex":            "--sandbox danger-full-access",
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
	require.Equal(t, harnesstest.FakeID, failed.Harness)
	require.Equal(t, "--dangerously-skip-permissions", failed.Flags)

	tmux.RespawnErr = nil

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
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("boom")
	st := newStore(t)

	launch := NewLaunchService(NewLaunchServiceParams{
		Tmux:               tmux,
		Agents:             st.Agents,
		Reviews:            reviewstore.NewReviewStore(st.Pool.GetDB()),
		Workflows:          st.Workflows,
		Tx:                 st.Pool,
		Launcher:           NewLauncher(NewLauncherParams{Tmux: tmux}),
		Harnesses:          harnesstest.Registry{Harness: &harnesstest.Fake{}},
		UserDefaultHarness: harnesstest.FakeID,
	})

	wf, err := st.Workflows.Create(t.Context(), workflowstore.Config{
		Name:     "workflow-" + uuid.NewString(),
		Harness:  harnesstest.FakeID,
		Flags:    "--model opus",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
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

	tmux.RespawnErr = nil

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
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("boom")
	st := newStore(t)

	launch := NewLaunchService(NewLaunchServiceParams{
		Tmux:               tmux,
		Agents:             st.Agents,
		Reviews:            reviewstore.NewReviewStore(st.Pool.GetDB()),
		Workflows:          st.Workflows,
		Tx:                 st.Pool,
		Launcher:           NewLauncher(NewLauncherParams{Tmux: tmux}),
		Harnesses:          harnesstest.Registry{Harness: &harnesstest.Fake{}, ByID: map[string]*harnesstest.Fake{"codex": {Name: "codex", NoSession: true}}},
		UserDefaultHarness: harnesstest.FakeID,
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

	tmux.RespawnErr = nil

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
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("boom")
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

	tmux.RespawnErr = nil

	_, err = manager.launch.RetryCreate(t.Context(), RetryCreateParams{
		ID:      failed.ID,
		Harness: harnesstest.FakeID,
	})
	require.NoError(t, err)

	got, err := st.Agents.GetByID(t.Context(), failed.ID)
	require.NoError(t, err)
	assert.Equal(t, failed.SessionID, got.SessionID, "naming the CLI it already runs changes nothing")
}

// TestRetryCreateFromThePanelOntoAnotherHarness covers the call the panel makes, which sends
// back the flags its form was prefilled with.
func TestRetryCreateFromThePanelOntoAnotherHarness(t *testing.T) {
	tests := []struct {
		name          string
		workflowFlags string
		edited        string
		want          string
	}{
		{
			name: "the prefilled flags come back unchanged, so the new CLI takes its own",
			want: "--sandbox danger-full-access",
		},
		{
			name:          "a workflow's flags go with the CLI it named",
			workflowFlags: "--model opus",
			want:          "--sandbox danger-full-access",
		},
		{
			name:   "flags the user actually typed are kept",
			edited: "--full-auto",
			want:   "--sandbox danger-full-access --full-auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			tmux.RespawnErr = errors.New("claude is not installed")
			st := newStore(t)

			launch := NewLaunchService(NewLaunchServiceParams{
				Tmux:               tmux,
				Agents:             st.Agents,
				Reviews:            reviewstore.NewReviewStore(st.Pool.GetDB()),
				Workflows:          st.Workflows,
				Tx:                 st.Pool,
				Launcher:           NewLauncher(NewLauncherParams{Tmux: tmux}),
				Harnesses:          harnesstest.Registry{Harness: &harnesstest.Fake{}},
				UserDefaultHarness: harnesstest.FakeID,
				UserDefaultFlags: map[string]string{
					harnesstest.FakeID: "--dangerously-skip-permissions",
					"codex":            "--sandbox danger-full-access",
				},
			})

			wf, err := st.Workflows.Create(t.Context(), workflowstore.Config{
				Name:     "workflow-" + uuid.NewString(),
				Harness:  harnesstest.FakeID,
				Flags:    tt.workflowFlags,
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
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

			tmux.RespawnErr = nil

			// The panel's form is prefilled with the flags the row already holds.
			sent := failed.Flags
			if tt.edited != "" {
				sent = tt.edited
			}

			_, err = launch.RetryCreate(t.Context(), RetryCreateParams{
				ID:      failed.ID,
				Harness: "codex",
				Flags:   sent,
			})
			require.NoError(t, err)

			got, err := st.Agents.GetByID(t.Context(), failed.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Flags)
		})
	}
}

// TestRetryCreateRefusesUnknownHarness covers a retry naming a CLI this binary cannot run.
func TestRetryCreateRefusesUnknownHarness(t *testing.T) {
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("boom")
	st := newStore(t)
	service := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}, Only: []string{harnesstest.FakeID}},
	})

	_, err := service.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "doomed",
		Dir:        t.TempDir(),
		WorkflowID: seedWorkflow(t, st),
	})
	require.Error(t, err)

	failed, err := st.Get(t.Context(), "doomed")
	require.NoError(t, err)

	_, err = service.launch.RetryCreate(t.Context(), RetryCreateParams{
		ID:      failed.ID,
		Harness: "gemini",
	})
	require.ErrorIs(t, err, harness.ErrNotRegistered)

	got, err := st.Agents.GetByID(t.Context(), failed.ID)
	require.NoError(t, err)
	assert.Equal(t, harnesstest.FakeID, got.Harness, "a CLI that cannot be run is not recorded")
}
