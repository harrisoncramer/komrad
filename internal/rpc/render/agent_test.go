package render

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	agentstore "weave/internal/store/agent"
	rowstore "weave/internal/store/row"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
)

// TestAgentCarriesWhatTheSyncersWrite covers the branch and the worktree removal flag, which a
// client cannot draw if the response never carries them.
func TestAgentCarriesWhatTheSyncersWrite(t *testing.T) {
	t.Parallel()

	rendered := Agent(rowOf(agentstore.Agent{
		ID:              "a1",
		Name:            "worker",
		Branch:          xptr.Ptr("fix/thing"),
		WorktreeRemoved: true,
	}), AgentOptions{})

	assert.Equal(t, "fix/thing", rendered.GetBranch())
	assert.True(t, rendered.GetWorktreeRemoved())
}

// TestAgentLeavesAPresentWorktreeAlone covers the other side of the flag: an agent whose cwd
// the syncer still finds is rendered without it.
func TestAgentLeavesAPresentWorktreeAlone(t *testing.T) {
	t.Parallel()

	rendered := Agent(rowOf(agentstore.Agent{ID: "a2", Name: "reviewer"}), AgentOptions{})

	assert.False(t, rendered.GetWorktreeRemoved())
}

// TestAgentCarriesTheStateChange pins the timestamp the panel counts a span from, which is
// written nowhere but this response.
func TestAgentCarriesTheStateChange(t *testing.T) {
	t.Parallel()

	changed := time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC)
	rendered := Agent(rowOf(agentstore.Agent{ID: "a1", StateChangedAt: changed}), AgentOptions{})

	assert.Equal(t, changed.Format(time.RFC3339), rendered.GetStateChangedAt())
}

// TestAgentWithoutAStateChange covers an agent that has never been given one, which sends
// nothing rather than a formatted zero time.
func TestAgentWithoutAStateChange(t *testing.T) {
	t.Parallel()

	assert.Empty(t, Agent(rowOf(agentstore.Agent{ID: "a2"}), AgentOptions{}).GetStateChangedAt())
}

// TestWatchedAgentCarriesOnlyTheOpeningOfAnArchivedPrompt pins that the watch draws an archived agent
// with the summary head of its prompt and a live one with the whole of it.
func TestWatchedAgentCarriesOnlyTheOpeningOfAnArchivedPrompt(t *testing.T) {
	t.Parallel()

	prompt := strings.Repeat("p", SummaryHead*2)
	archived := WatchedAgent(rowOf(agentstore.Agent{
		ID:         "archived",
		Prompt:     prompt,
		ArchivedAt: xptr.Ptr(time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)),
	}))
	live := WatchedAgent(rowOf(agentstore.Agent{
		ID:     "live",
		Prompt: prompt,
	}))

	assert.Len(t, archived.GetPrompt(), SummaryHead, "the archived agent's prompt is cut to its opening")
	assert.True(t, archived.GetPromptTruncated())
	assert.Equal(t, prompt, live.GetPrompt(), "and the live agent's is whole")
	assert.False(t, live.GetPromptTruncated())
}

// rowOf is an agent's row with nothing beside it.
func rowOf(stored agentstore.Agent) rowstore.Row {
	return rowstore.Row{Agent: stored}
}

// TestAgentStateOf checks every stored state lands on its own wire value.
func TestAgentStateOf(t *testing.T) {
	t.Parallel()

	states := map[agentstore.State]agentv1.AgentState{
		agentstore.StateIdle:            agentv1.AgentState_AGENT_STATE_IDLE,
		agentstore.StateThinking:        agentv1.AgentState_AGENT_STATE_THINKING,
		agentstore.StateWaiting:         agentv1.AgentState_AGENT_STATE_WAITING,
		agentstore.StateSettingUp:       agentv1.AgentState_AGENT_STATE_SETTING_UP,
		agentstore.StateFailedToCreate:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
		agentstore.StateFailedToCleanup: agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP,
		agentstore.StateUncertain:       agentv1.AgentState_AGENT_STATE_UNCERTAIN,
	}
	for stored, wire := range states {
		assert.Equal(t, wire, AgentStateOf(stored), string(stored))
	}
	assert.Equal(t, agentv1.AgentState_AGENT_STATE_UNSPECIFIED, AgentStateOf(""))
}

// TestAgentRoleOf checks every stored role lands on its own wire value.
func TestAgentRoleOf(t *testing.T) {
	t.Parallel()

	roles := map[agentstore.Role]agentv1.AgentRole{
		agentstore.RoleImplementer:  agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER,
		agentstore.RoleReviewer:     agentv1.AgentRole_AGENT_ROLE_REVIEWER,
		agentstore.RoleOrchestrator: agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
	}
	for stored, wire := range roles {
		assert.Equal(t, wire, AgentRoleOf(stored), string(stored))
	}
	assert.Equal(t, agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED, AgentRoleOf(""))
}
