package agents

import (
	"errors"
	"strings"
	"testing"
	"time"

	"komrad/internal/agenterr"
	"komrad/internal/harnesstest"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	"komrad/internal/tasks"
	"komrad/internal/tmuxtest"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRestartAgentStartsANewConversation pins a writer relaunched on a new conversation, briefed from its task,
// on a session it has not held before.
func TestRestartAgentStartsANewConversation(t *testing.T) {
	const earlier = "0f0b6f2c-6f8f-4a0e-9c2f-2a8f9d0a1b22"

	st := newStore(t)
	tmux := tmuxtest.NewFake()
	writer := seedRunning(t, st, tmux, "writer", "%1", agentv1.AgentState_AGENT_STATE_IDLE)
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

// TestRestartAgentStartsAnOrchestratorOnANewConversation pins an orchestrator relaunched on a new
// conversation from the panel, briefed as its workflow's orchestrator.
func TestRestartAgentStartsAnOrchestratorOnANewConversation(t *testing.T) {
	const earlier = "5b1d9a7e-2c4f-4f0a-8e3d-7a6c5b4e3d21"

	st := newStore(t)
	tmux := tmuxtest.NewFake()
	orchestrator := seedOrchestrator(t, st, tmux)
	require.NoError(t, st.Agents.SetSessionID(t.Context(), orchestrator.ID, earlier))
	service, fakeHarness := newManagerWithHarness(tmux, st, "")

	restarted, err := service.resume.RestartAgent(t.Context(), RestartAgentParams{ID: orchestrator.ID})
	require.NoError(t, err)

	require.Len(t, fakeHarness.Launches, 1)
	launch := fakeHarness.Launches[0]
	assert.False(t, launch.Resume)
	assert.Equal(t, orchestratorRestartPrompt, launch.Prompt)
	assert.Contains(t, launch.Context, strings.TrimSpace(orchestratorDoc))
	assert.NotContains(t, launch.Context, strings.TrimSpace(writerDoc))
	assert.NotEqual(t, earlier, launch.SessionID)
	assert.Equal(t, launch.SessionID, restarted.SessionID)
	assert.Contains(t, tmux.KilledPanes, orchestrator.Pane)
}

// TestRestartAgentOntoAnotherHarnessOrFlags pins the CLI and flags a restarted writer runs on, given
// the ones the panel's form sends back.
func TestRestartAgentOntoAnotherHarnessOrFlags(t *testing.T) {
	tests := []struct {
		name        string
		harness     string
		flags       string
		wantHarness string
		wantFlags   string
	}{
		{
			name:        "another CLI drops the flags written for the one it left",
			harness:     "codex",
			flags:       "--model opus",
			wantHarness: "codex",
			wantFlags:   "",
		},
		{
			name:        "another CLI keeps flags typed for it",
			harness:     "codex",
			flags:       "--model o3",
			wantHarness: "codex",
			wantFlags:   "--model o3",
		},
		{
			name:        "another model on the same CLI",
			harness:     harnesstest.FakeID,
			flags:       "--model sonnet",
			wantHarness: harnesstest.FakeID,
			wantFlags:   "--model sonnet",
		},
		{
			name:        "the same CLI and flags",
			harness:     harnesstest.FakeID,
			flags:       "--model opus",
			wantHarness: harnesstest.FakeID,
			wantFlags:   "--model opus",
		},
		{
			name:        "nothing named",
			wantHarness: harnesstest.FakeID,
			wantFlags:   "--model opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()
			writer := seedRunning(t, st, tmux, "writer", "%1", agentv1.AgentState_AGENT_STATE_IDLE)
			require.NoError(t, st.Agents.SetHarness(t.Context(), agentstore.SetHarnessParams{
				ID:      writer.ID,
				Harness: harnesstest.FakeID,
				Flags:   "--model opus",
			}))
			service, fakeHarness := newManagerWithHarness(tmux, st, "")

			restarted, err := service.resume.RestartAgent(t.Context(), RestartAgentParams{
				ID:      writer.ID,
				Harness: tt.harness,
				Flags:   tt.flags,
			})
			require.NoError(t, err)

			require.Len(t, fakeHarness.Launches, 1)
			assert.Equal(t, tt.wantFlags, fakeHarness.Launches[0].Flags)
			assert.Equal(t, tt.wantHarness, restarted.Harness)
			assert.Equal(t, tt.wantFlags, restarted.Flags)
		})
	}
}

// TestRestartAgentGivingUpKeepsTheEarlierConversation pins a writer whose restart fails left on the
// conversation, CLI and flags it had.
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
			writer := seedRunning(t, st, tmux, "writer", "%1", agentv1.AgentState_AGENT_STATE_IDLE)
			require.NoError(t, st.Agents.SetHarness(t.Context(), agentstore.SetHarnessParams{
				ID:        writer.ID,
				Harness:   harnesstest.FakeID,
				Flags:     "--model opus",
				SessionID: earlier,
			}))
			service, _ := newManagerWithHarness(tmux, st, "")
			tt.fail(tmux)

			_, err := service.resume.RestartAgent(t.Context(), RestartAgentParams{
				ID:      writer.ID,
				Harness: "codex",
				Flags:   "--model o3",
			})

			require.Error(t, err)
			got, err := st.Agents.GetByID(t.Context(), writer.ID)
			require.NoError(t, err)
			assert.Equal(t, earlier, got.SessionID)
			assert.Equal(t, harnesstest.FakeID, got.Harness)
			assert.Equal(t, "--model opus", got.Flags)
		})
	}
}

// TestRestartAgentRefuses pins which agents a restart turns away.
func TestRestartAgentRefuses(t *testing.T) {
	tests := []struct {
		name      string
		restart   string
		by        string
		wantErrIs error
	}{
		{
			name:      "a reviewer",
			restart:   "reviewer",
			wantErrIs: errRestartRole,
		},
		{
			name:      "an orchestrator asked by an agent",
			restart:   "orchestrator",
			by:        "orchestrator",
			wantErrIs: errRestartOrchestratorByAgent,
		},
		{
			name:      "an archived writer",
			restart:   "archived writer",
			wantErrIs: agenterr.ErrAgentArchived,
		},
		{
			name:      "a writer whose worktree was removed",
			restart:   "removed writer",
			wantErrIs: tasks.ErrWorktreeRemoved,
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
			require.NoError(t, archiveWriters(t.Context(), service.lifecycle, []uuid.UUID{archived.ID}))
			removed := seedRunningIn(t, st, tmux, orchestrator.WorkflowID, "removed writer", "%3")
			require.NoError(t, st.Tasks.SetHead(t.Context(), *removed.TaskID, taskstore.SetHeadParams{WorktreeRemovalReason: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE}))
			seeded := map[string]uuid.UUID{
				"orchestrator":    orchestrator.ID,
				"reviewer":        seedReviewer(t, st, tmux, writer).ID,
				"archived writer": archived.ID,
				"removed writer":  removed.ID,
			}

			_, err := service.resume.RestartAgent(t.Context(), RestartAgentParams{ID: seeded[tt.restart], ByAgentID: seeded[tt.by]})

			require.ErrorIs(t, err, tt.wantErrIs)
			assert.Empty(t, fakeHarness.Launches)
		})
	}
}
