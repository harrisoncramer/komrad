package agents

import (
	"testing"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgentResolvesHarness covers which CLI a new agent lands on. It follows the
// same precedence the directory and flags do, what the caller asked for ahead of the
// workflow's default and the configured one, and the answer is recorded on the row
// so every later pass resolves the same harness.
func TestNewAgentResolvesHarness(t *testing.T) {
	tests := []struct {
		name string
		// requested is the harness the create names, and workflow is the default of
		// the bucket it lands in.
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
			// Every id resolves here, so this is about which one is chosen.
			service := st.agentService(tmux, fakeHarnesses{harness: &fakeHarness{}}, "", "")

			created, err := st.Workflows.Create(t.Context(), workflowstore.Config{
				Name:    "workflow-" + uuid.NewString(),
				Harness: tt.workflow,
			})
			require.NoError(t, err)

			_, err = service.NewAgent(t.Context(), NewAgentParams{
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

// TestNewAgentRefusesUnknownHarness covers an agent naming a CLI this binary cannot
// run. It is refused where the directory and the workflow are, before a window is
// opened or a row written, since nothing about it gets better later.
func TestNewAgentRefusesUnknownHarness(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	service := st.agentService(tmux, fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}}, "", "")

	_, err := service.NewAgent(t.Context(), NewAgentParams{
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
	assert.Empty(t, tmux.sent, "and types nothing")
}

// TestRestoreRefusesUnknownHarness covers a dormant agent recorded against a CLI the
// binary no longer runs, which a removed harness or a downgrade looks like. Bringing
// it back would put the wrong CLI on its conversation, so the restore fails and says
// which harness is missing.
func TestRestoreRefusesUnknownHarness(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	service := st.agentService(tmux, fakeHarnesses{harness: &fakeHarness{}, only: []string{testHarnessID}}, "", "")

	require.NoError(t, st.Create(t.Context(), agentstore.Agent{
		Name:      "dormant",
		State:     agentstore.StateIdle,
		Harness:   "gemini",
		SessionID: "sess-1",
	}))
	stored, err := st.Get(t.Context(), "dormant")
	require.NoError(t, err)

	_, err = service.RestoreAgent(t.Context(), stored.ID)

	require.ErrorIs(t, err, harness.ErrNotRegistered)
	assert.ErrorContains(t, err, "gemini")
	assert.Empty(t, tmux.newWindows, "nothing is opened for an agent that cannot be run")
}

// TestNewAgentFlagsFollowTheHarness covers where a new agent's default flags come
// from. Flag vocabularies do not overlap between CLIs, so an agent inherits the
// flags configured for the one it runs and nobody else's.
func TestNewAgentFlagsFollowTheHarness(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()

	service := NewAgentService(AgentParams{
		Tmux:           tmux,
		Agents:         st.Agents,
		Workflows:      st.Workflows,
		Tx:             st.Pool,
		Launcher:       NewLauncher(LauncherParams{Tmux: tmux}),
		Harnesses:      fakeHarnesses{harness: &fakeHarness{}},
		DefaultHarness: testHarnessID,
		DefaultFlags: map[string]string{
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
