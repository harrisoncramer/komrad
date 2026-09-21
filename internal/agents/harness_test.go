package agents

import (
	"testing"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgentGetsHarness pins which CLI a new agent lands on, taking what the caller asked
// for ahead of the workflow's default and the configured one.
func TestNewAgentGetsHarness(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		workflow  string
		want      string
	}{
		{
			name: "neither the agent nor its workflow names one, so it takes the configured default",
			want: testHarnessID,
		},
		{
			name:     "the workflow's default when the agent names none",
			workflow: "codex",
			want:     "codex",
		},
		{
			name:      "what the caller asked for beats the workflow",
			requested: "codex",
			workflow:  testHarnessID,
			want:      "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()

			service := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: fakeHarnesses{harness: &fakeHarness{}},
			})

			created, err := st.Workflows.Create(t.Context(), workflowstore.Config{
				Name:     "workflow-" + uuid.NewString(),
				Harness:  tt.workflow,
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
			})
			require.NoError(t, err)

			_, err = service.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				Harness:    tt.requested,
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			stored, err := st.Get(t.Context(), "agent")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tt.want, stored.Harness)
		})
	}
}

// TestWorkflowFlagsFollowItsHarness pins that a workflow's flags reach only the agents running
// the CLI it names.
func TestWorkflowFlagsFollowItsHarness(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{
			name: "an agent on the workflow's own CLI takes its flags",
			want: "--default-flag --model opus",
		},
		{
			name:      "an agent on another CLI is launched without them",
			requested: "codex",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()

			service := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: fakeHarnesses{harness: &fakeHarness{}},
				flags:     "--default-flag",
			})

			created, err := st.Workflows.Create(t.Context(), workflowstore.Config{
				Name:     "workflow-" + uuid.NewString(),
				Harness:  testHarnessID,
				Flags:    "--model opus",
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
			})
			require.NoError(t, err)

			_, err = service.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				Harness:    tt.requested,
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			stored, err := st.Get(t.Context(), "agent")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tt.want, stored.Flags)
		})
	}
}

// TestNewAgentRefusesUnknownHarness pins that a CLI this binary cannot run is refused before
// a window is opened or a row written.
func TestNewAgentRefusesUnknownHarness(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	service := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}},
	})

	_, err := service.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		Harness:    "gemini",
		WorkflowID: seedWorkflow(t, st),
	})

	require.ErrorIs(t, err, harness.ErrNotRegistered)

	agents, listErr := st.List(t.Context())
	require.NoError(t, listErr)
	assert.Empty(t, agents, "a refused harness records nothing")
	assert.Empty(t, tmux.newWindows, "and opens no window")
	assert.Empty(t, tmux.respawned, "and starts nothing")
}

// TestResumeRefusesUnknownHarness pins that a resume fails rather than putting the wrong CLI
// on a dormant agent's conversation.
func TestResumeRefusesUnknownHarness(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	service := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}},
	})

	require.NoError(t, st.Create(t.Context(), agentstore.Agent{
		Name:      "dormant",
		State:     agentstore.StateIdle,
		Harness:   "gemini",
		SessionID: "sess-1",
	}))
	stored, err := st.Get(t.Context(), "dormant")
	require.NoError(t, err)

	_, err = service.resume.ResumeAgent(t.Context(), stored.ID)

	require.ErrorIs(t, err, harness.ErrNotRegistered)
	require.ErrorContains(t, err, "gemini")
	assert.Empty(t, tmux.newWindows, "nothing is opened for an agent that cannot be run")
}

// TestNewAgentFlagsFollowTheHarness pins that an agent inherits only the flags configured for
// the CLI it runs.
func TestNewAgentFlagsFollowTheHarness(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()

	service := NewLaunchService(NewLaunchServiceParams{
		Tmux:               tmux,
		Agents:             st.Agents,
		Reviews:            reviewstore.NewReviewStore(st.Pool.GetDB()),
		Workflows:          st.Workflows,
		Tx:                 st.Pool,
		Launcher:           NewLauncher(NewLauncherParams{Tmux: tmux}),
		Harnesses:          fakeHarnesses{harness: &fakeHarness{}},
		UserDefaultHarness: testHarnessID,
		UserDefaultFlags: map[string]string{
			testHarnessID: "--model opus",
			"codex":       "--sandbox read-only",
		},
	})

	_, err := service.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		Harness:    "codex",
		Flags:      "--extra",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	stored, err := st.Get(t.Context(), "agent")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "--sandbox read-only --extra", stored.Flags,
		"the flags configured for the harness it runs, then its own")
}
