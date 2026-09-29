package server

import (
	"context"
	"errors"
	"fmt"

	annotationv1 "komrad/proto/gen/annotation/v1"

	"komrad/internal/rpcapi"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
)

// toolRoles maps what an agent was launched to do onto the annotation the tools declare.
var toolRoles = map[agentstore.Role]annotationv1.Role{
	agentstore.RoleImplementer:  annotationv1.Role_ROLE_IMPLEMENTER,
	agentstore.RoleReviewer:     annotationv1.Role_ROLE_REVIEWER,
	agentstore.RoleOrchestrator: annotationv1.Role_ROLE_ORCHESTRATOR,
}

// newRoleOf reads the role of an agent making a call.
func newRoleOf(agents *agentstore.AgentStore) rpcapi.RoleOf {
	return func(ctx context.Context, agentID string) (annotationv1.Role, error) {
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
