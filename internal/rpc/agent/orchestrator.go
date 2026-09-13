package agent

import (
	"context"

	agentv1 "weave/proto/gen/agent/v1"

	"weave/internal/rpcerr"
	agentstore "weave/internal/store/agent"

	"connectrpc.com/connect"
)

type orchestratorStarter interface {
	Start(ctx context.Context, workflowID string) (*agentstore.Agent, error)
}

// StartOrchestrator returns a workflow's orchestrator with a pane to talk to, starting the one
// it has never had.
func (s *Service) StartOrchestrator(
	ctx context.Context,
	req *connect.Request[agentv1.StartOrchestratorRequest],
) (*connect.Response[agentv1.StartOrchestratorResponse], error) {
	started, err := s.orchestrator.Start(ctx, req.Msg.GetWorkflowId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to start the orchestrator")
	}

	rendered, err := s.renderOneAgent(ctx, *started)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&agentv1.StartOrchestratorResponse{Agent: rendered}), nil
}
