package render

import (
	agentstore "weave/internal/store/agent"
	agentv1 "weave/proto/gen/agent/v1"
)

// AgentStateOf is a stored state as the wire carries it.
func AgentStateOf(state agentstore.State) agentv1.AgentState {
	switch state {
	case agentstore.StateIdle:
		return agentv1.AgentState_AGENT_STATE_IDLE
	case agentstore.StateThinking:
		return agentv1.AgentState_AGENT_STATE_THINKING
	case agentstore.StateWaiting:
		return agentv1.AgentState_AGENT_STATE_WAITING
	case agentstore.StateSettingUp:
		return agentv1.AgentState_AGENT_STATE_SETTING_UP
	case agentstore.StateFailedToCreate:
		return agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
	case agentstore.StateFailedToCleanup:
		return agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
	case agentstore.StateUncertain:
		return agentv1.AgentState_AGENT_STATE_UNCERTAIN
	}

	return agentv1.AgentState_AGENT_STATE_UNSPECIFIED
}

// AgentRoleOf is a stored role as the wire carries it.
func AgentRoleOf(role agentstore.Role) agentv1.AgentRole {
	switch role {
	case agentstore.RoleImplementer:
		return agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER
	case agentstore.RoleReviewer:
		return agentv1.AgentRole_AGENT_ROLE_REVIEWER
	case agentstore.RoleOrchestrator:
		return agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR
	}

	return agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED
}
