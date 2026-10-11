package agents

import (
	"errors"
	"testing"
	"time"

	"komrad/internal/agenterr"
	agentstore "komrad/internal/store/agent"
	"komrad/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRestartAgentStartsANewConversation pins a writer relaunched fresh, briefed from its task,
// on a session it has not held before.
func TestRestartAgentStartsANewConversation(t *testing.T) {
	const earlier = "0f0b6f2c-6f8f-4a0e-9c2f-2a8f9d0a1b22"

	st := newStore(t)
	tmux := tmuxtest.NewFake()
	writer := seedRunning(t, st, tmux, "writer", "%1", agentstore.StateIdle)
	require.NoError(t, st.Agents.SetSessionID(t.Context(), writer.ID, earlier))
	tmux.AfterKillPane = func(string) {
		at := time.Now()
		require.NoError(t, st.Agents.SetInterruptedAtByID(t.Context(), writer.ID, &at))
	}
	service, fakeHarness := newManagerWithHarness(tmux, st, "")

	restarted, err := service.resume.RestartAgent(t.Context(), RestartAgentParams{ID: writer.ID, Note: " pick up the review "})
	require.NoError(t, err)

	require.Len(t, fakeHarness.Launches, 1)
	launch := fakeHarness.Launches[0]
	assert.False(t, launch.Resume, "the writer starts a conversation rather than carrying on the earlier one")
	assert.Equal(t, restartPrompt+"\n\npick up the review", launch.Prompt)
	assert.NotEqual(t, earlier, launch.SessionID)
	assert.Equal(t, launch.SessionID, restarted.SessionID)
	assert.Contains(t, tmux.KilledPanes, "%1", "the earlier pane goes")
	assert.Nil(t, restarted.InterruptedAt, "a new conversation has no cut-off turn")
}

// TestRestartAgentGivingUpKeepsTheEarlierConversation pins a writer whose restart fails left on the
// conversation it had.
func TestRestartAgentGivingUpKeepsTheEarlierConversation(t *testing.T) {
	const earlier = "0f0b6f2c-6f8f-4a0e-9c2f-2a8f9d0a1b22"

	tests := []struct {
		name string
		fail func(tmux *tmuxtest.Fake)
	}{
		{
			name: "a window that does not open",
			fail: func(tmux *tmuxtest.Fake) { tmux.NewWindowErr = errors.New("no window") },
		},
		{
			name: "a launch that fails",
			fail: func(tmux *tmuxtest.Fake) { tmux.RespawnErr = errors.New("no respawn") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			writer := seedRunning(t, st, tmux, "writer", "%1", agentstore.StateIdle)
			require.NoError(t, st.Agents.SetSessionID(t.Context(), writer.ID, earlier))
			service, _ := newManagerWithHarness(tmux, st, "")
			tt.fail(tmux)

			_, err := service.resume.RestartAgent(t.Context(), RestartAgentParams{ID: writer.ID})

			require.Error(t, err)
			got, err := st.Agents.GetByID(t.Context(), writer.ID)
			require.NoError(t, err)
			assert.Equal(t, earlier, got.SessionID)
		})
	}
}

// TestRestartAgentRefuses pins which agents a restart turns away.
func TestRestartAgentRefuses(t *testing.T) {
	tests := []struct {
		name      string
		restart   string
		wantErrIs error
	}{
		{
			name:      "a reviewer",
			restart:   "reviewer",
			wantErrIs: errRestartNotWriter,
		},
		{
			name:      "an orchestrator",
			restart:   "orchestrator",
			wantErrIs: errRestartNotWriter,
		},
		{
			name:      "an archived writer",
			restart:   "archived writer",
			wantErrIs: agenterr.ErrAgentArchived,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			service, fakeHarness := newManagerWithHarness(tmux, st, "")

			orchestrator := seedOrchestrator(t, st, tmux)
			writer := seedRunningIn(t, st, tmux, orchestrator.WorkflowID, "writer", "%1")
			archived := seedRunningIn(t, st, tmux, orchestrator.WorkflowID, "archived writer", "%2")
			require.NoError(t, archiveWriters(t.Context(), service.lifecycle, []string{archived.ID}))
			seeded := map[string]string{
				"orchestrator":    orchestrator.ID,
				"reviewer":        seedChild(t, st, tmux, writer).ID,
				"archived writer": archived.ID,
			}

			_, err := service.resume.RestartAgent(t.Context(), RestartAgentParams{ID: seeded[tt.restart]})

			require.ErrorIs(t, err, tt.wantErrIs)
			assert.Empty(t, fakeHarness.Launches)
		})
	}
}
