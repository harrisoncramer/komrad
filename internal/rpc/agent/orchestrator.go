package agent

import (
	"context"

	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1komrad"

	"komrad/internal/fail"
	"komrad/internal/rpcerr"
	agentstore "komrad/internal/store/agent"

	"github.com/google/uuid"
)

type orchestratorStarter interface {
	Start(ctx context.Context, workflowID uuid.UUID, prompt string) (*agentstore.Agent, error)
}

// StartOrchestrator returns a workflow's orchestrator with a pane to talk to, starting the one
// it has never had.
func (s *Handler) StartOrchestrator(
	ctx context.Context,
	req *agentv1komrad.StartOrchestratorRequest,
) (*agentv1.StartOrchestratorResponse, *fail.Error) {
	started, err := s.orchestrator.Start(ctx, req.WorkflowId, req.Prompt)
	if err != nil {
		return nil, rpcerr.From(err, "failed to start the orchestrator")
	}

	rendered, failure := s.renderOneAgent(ctx, started.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.StartOrchestratorResponse{Agent: rendered}, nil
}
