package render

import (
	"testing"

	"github.com/stretchr/testify/assert"

	agentstore "weave/internal/store/agent"
	agentv1 "weave/proto/gen/agent/v1"
)

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
