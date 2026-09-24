package server

import (
	"context"
	"errors"
	"log/slog"

	annotationv1 "komrade/proto/gen/annotation/v1"

	"komrade/internal/logging"
	"komrade/internal/mcp"
	"komrade/internal/store"
	agentstore "komrade/internal/store/agent"
)

// toolRoles maps what an agent was launched to do onto the annotation the tools declare.
var toolRoles = map[agentstore.Role]annotationv1.Role{
	agentstore.RoleImplementer:  annotationv1.Role_ROLE_IMPLEMENTER,
	agentstore.RoleReviewer:     annotationv1.Role_ROLE_REVIEWER,
	agentstore.RoleOrchestrator: annotationv1.Role_ROLE_ORCHESTRATOR,
}

// newRoleOf reads the role of an agent making an MCP call, unspecified for one it cannot read.
func newRoleOf(agents *agentstore.AgentStore, logger *slog.Logger) mcp.RoleOf {
	return func(ctx context.Context, agentID string) annotationv1.Role {
		agent, err := agents.GetByID(ctx, agentID)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				logger.LogAttrs(ctx, slog.LevelWarn, "failed to read the role of an mcp caller",
					append(logging.Err(err), slog.String(logging.AgentID, agentID))...)
			}

			return annotationv1.Role_ROLE_UNSPECIFIED
		}

		return toolRoles[agent.Role]
	}
}
