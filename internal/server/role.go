package server

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/rpcapi"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	agentv1 "komrad/proto/gen/agent/v1"
	annotationv1 "komrad/proto/gen/annotation/v1"

	"github.com/google/uuid"
)

// toolRoles maps what komrad launched an agent to do onto the annotation the tools declare.
var toolRoles = map[agentv1.AgentRole]annotationv1.Role{
	agentv1.AgentRole_AGENT_ROLE_WRITER:       annotationv1.Role_ROLE_WRITER,
	agentv1.AgentRole_AGENT_ROLE_REVIEWER:     annotationv1.Role_ROLE_REVIEWER,
	agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR: annotationv1.Role_ROLE_ORCHESTRATOR,
}

// newRoleOf reads the role of an agent making a call.
func newRoleOf(agents *agentstore.AgentStore) rpcapi.RoleOf {
	return func(ctx context.Context, agentID uuid.UUID) (annotationv1.Role, error) {
		agent, err := agents.GetByID(ctx, agentID)
		if errors.Is(err, store.ErrNotFound) {
			return annotationv1.Role_ROLE_UNSPECIFIED, rpcapi.ErrUnknownCaller.Saying("komrad knows no agent %q", agentID)
		}
		if err != nil {
			return annotationv1.Role_ROLE_UNSPECIFIED, fmt.Errorf("failed to read the role of agent %q: %w", agentID, err)
		}

		return toolRoles[agent.Role], nil
	}
}
