package server

import (
	"context"
	"errors"
	"log/slog"

	annotationv1 "weave/proto/gen/annotation/v1"

	"weave/internal/logging"
	"weave/internal/mcp"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
)

// toolRoles maps what an agent was launched to do onto the annotation the tools declare.
var toolRoles = map[agentstore.Role]annotationv1.Role{
	agentstore.RoleImplementer:  annotationv1.Role_ROLE_IMPLEMENTER,
	agentstore.RoleReviewer:     annotationv1.Role_ROLE_REVIEWER,
	agentstore.RoleOrchestrator: annotationv1.Role_ROLE_ORCHESTRATOR,
}

// roleOf places an agent making an MCP call so it is served the tools for the job it holds,
// falling back to every tool for one weave cannot read.
func roleOf(agents *agentstore.AgentStore, logger *slog.Logger) mcp.RoleOf {
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
