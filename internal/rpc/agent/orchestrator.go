package agent

import (
	"context"

	agentv1 "komrad/proto/gen/agent/v1"

	"komrad/internal/fail"
	"komrad/internal/rpcerr"
	agentstore "komrad/internal/store/agent"
)

type orchestratorStarter interface {
	Start(ctx context.Context, workflowID string) (*agentstore.Agent, error)
}

// StartOrchestrator returns a workflow's orchestrator with a pane to talk to, starting the one
// it has never had.
func (s *Handler) StartOrchestrator(
	ctx context.Context,
	req *agentv1.StartOrchestratorRequest,
) (*agentv1.StartOrchestratorResponse, *fail.Error) {
	started, err := s.orchestrator.Start(ctx, req.GetWorkflowId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to start the orchestrator")
	}

	rendered, failure := s.renderOneAgent(ctx, started.ID)
	if failure != nil {
		return nil, failure
	}

	return &agentv1.StartOrchestratorResponse{Agent: rendered}, nil
}
