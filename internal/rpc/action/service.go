// Package action serves ActionService over ConnectRPC.
package action

import (
	"context"
	"errors"

	"weave/internal/actions"
	"weave/internal/agents"
	actionv1 "weave/proto/gen/action/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// actionManager is the part of the action service the handlers drive. The handlers translate
// and nothing more. What an action is and what it is told lives behind this.
type actionManager interface {
	ListActions() ([]agents.ListedAction, error)
	RunAction(ctx context.Context, agentID, name string) error
}

// Service implements actionv1connect.ActionServiceHandler.
type Service struct {
	actions actionManager
}

// NewService constructs the handler from the service that owns the rules.
func NewService(manager actionManager) *Service {
	return &Service{actions: manager}
}

// ListActions returns every file in the user's actions directory. A directory that is not there
// is an empty listing rather than an error, since not having written an action is the ordinary
// case and a caller learns the same thing either way.
func (s *Service) ListActions(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[actionv1.ListActionsResponse], error) {
	listed, err := s.actions.ListActions()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	resources := make([]*actionv1.Action, 0, len(listed))
	for _, action := range listed {
		resources = append(resources, &actionv1.Action{
			Name:             action.Name,
			Executable:       action.Executable,
			Preview:          action.Preview,
			PreviewTruncated: action.Truncated,
		})
	}

	return connect.NewResponse(&actionv1.ListActionsResponse{Actions: resources}), nil
}

// RunAction fires an action and answers as soon as the process exists. The response is empty
// because there is nothing to say: nothing waits for the action and nothing reads what it did.
//
// The failures are all of them from before the process started. A missing agent or action is
// NotFound. A file the user has not made executable is FailedPrecondition, since the file is
// there and one chmod away from running, and so is an agent whose directory has been removed
// from under it, since nothing about the action itself is wrong.
func (s *Service) RunAction(ctx context.Context, req *connect.Request[actionv1.RunActionRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.actions.RunAction(ctx, req.Msg.GetAgentId(), req.Msg.GetAction())

	switch {
	case err == nil:
		return connect.NewResponse(&emptypb.Empty{}), nil
	case errors.Is(err, agents.ErrAgentNotFound), errors.Is(err, agents.ErrActionNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, actions.ErrNotExecutable), errors.Is(err, actions.ErrNotStartable),
		errors.Is(err, actions.ErrDirMissing):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	default:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
}
