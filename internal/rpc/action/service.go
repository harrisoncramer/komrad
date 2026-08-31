package action

import (
	"context"
	"time"

	"weave/internal/agents"
	"weave/internal/rpcerr"
	actionv1 "weave/proto/gen/action/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type actionRunner interface {
	ListActions() ([]agents.Action, error)
	RunAction(ctx context.Context, agentID, name string) error
}

type Service struct {
	actions actionRunner
}

// NewService constructs the handler from the service that owns the rules.
func NewService(manager actionRunner) *Service {
	return &Service{actions: manager}
}

// ListActions returns every file in the user's actions directory, and a directory that is not
// there is an empty listing rather than an error.
func (s *Service) ListActions(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[actionv1.ListActionsResponse], error) {
	listed, err := s.actions.ListActions()
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list actions")
	}

	resources := make([]*actionv1.Action, 0, len(listed))
	for _, action := range listed {
		resources = append(resources, &actionv1.Action{
			Name:           action.Name,
			Executable:     action.Executable,
			Description:    action.Header.Description,
			Timeout:        declaredTimeout(action.Header.Timeout),
			HeaderProblems: action.Header.Problems,
		})
	}

	return connect.NewResponse(&actionv1.ListActionsResponse{Actions: resources}), nil
}

// RunAction fires an action and answers as soon as the process exists, so the response is
// empty and every failure it can report happened before the process started.
func (s *Service) RunAction(ctx context.Context, req *connect.Request[actionv1.RunActionRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.actions.RunAction(ctx, req.Msg.GetAgentId(), req.Msg.GetAction()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to run the action")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func declaredTimeout(timeout time.Duration) *durationpb.Duration {
	if timeout <= 0 {
		return nil
	}

	return durationpb.New(timeout)
}
