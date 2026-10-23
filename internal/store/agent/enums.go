package agent

import (
	"fmt"

	"komrad/db/models"
	agentv1 "komrad/proto/gen/agent/v1"
)

// fromServiceToDBAgentState is the stored value of an agent state.
func fromServiceToDBAgentState(state agentv1.AgentState) (models.AgentState, error) {
	switch state {
	case agentv1.AgentState_AGENT_STATE_IDLE:
		return models.AgentStateIdle, nil
	case agentv1.AgentState_AGENT_STATE_THINKING:
		return models.AgentStateThinking, nil
	case agentv1.AgentState_AGENT_STATE_WAITING:
		return models.AgentStateWaiting, nil
	case agentv1.AgentState_AGENT_STATE_SETTING_UP:
		return models.AgentStateSettingUp, nil
	case agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE:
		return models.AgentStateFailedToCreate, nil
	case agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP:
		return models.AgentStateFailedToCleanup, nil
	case agentv1.AgentState_AGENT_STATE_UNCERTAIN:
		return models.AgentStateUncertain, nil
	case agentv1.AgentState_AGENT_STATE_ERRORED:
		return models.AgentStateErrored, nil
	case agentv1.AgentState_AGENT_STATE_UNSPECIFIED:
	}

	return "", fmt.Errorf("agent state %v has no stored value", state)
}

// fromDBToServiceAgentState is the agent state a stored value names.
func fromDBToServiceAgentState(state models.AgentState) (agentv1.AgentState, error) {
	switch state {
	case models.AgentStateIdle:
		return agentv1.AgentState_AGENT_STATE_IDLE, nil
	case models.AgentStateThinking:
		return agentv1.AgentState_AGENT_STATE_THINKING, nil
	case models.AgentStateWaiting:
		return agentv1.AgentState_AGENT_STATE_WAITING, nil
	case models.AgentStateSettingUp:
		return agentv1.AgentState_AGENT_STATE_SETTING_UP, nil
	case models.AgentStateFailedToCreate:
		return agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE, nil
	case models.AgentStateFailedToCleanup:
		return agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP, nil
	case models.AgentStateUncertain:
		return agentv1.AgentState_AGENT_STATE_UNCERTAIN, nil
	case models.AgentStateErrored:
		return agentv1.AgentState_AGENT_STATE_ERRORED, nil
	}

	return agentv1.AgentState_AGENT_STATE_UNSPECIFIED, fmt.Errorf("stored agent state %q has no service value", state)
}

// fromServiceToDBAgentRole is the stored value of an agent role.
func fromServiceToDBAgentRole(role agentv1.AgentRole) (models.AgentRole, error) {
	switch role {
	case agentv1.AgentRole_AGENT_ROLE_WRITER:
		return models.AgentRoleWriter, nil
	case agentv1.AgentRole_AGENT_ROLE_REVIEWER:
		return models.AgentRoleReviewer, nil
	case agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR:
		return models.AgentRoleOrchestrator, nil
	case agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED:
	}

	return "", fmt.Errorf("agent role %v has no stored value", role)
}

// fromDBToServiceAgentRole is the agent role a stored value names.
func fromDBToServiceAgentRole(role models.AgentRole) (agentv1.AgentRole, error) {
	switch role {
	case models.AgentRoleWriter:
		return agentv1.AgentRole_AGENT_ROLE_WRITER, nil
	case models.AgentRoleReviewer:
		return agentv1.AgentRole_AGENT_ROLE_REVIEWER, nil
	case models.AgentRoleOrchestrator:
		return agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR, nil
	}

	return agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED, fmt.Errorf("stored agent role %q has no service value", role)
}
