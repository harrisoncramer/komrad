// Package action serves ActionService over ConnectRPC.
package action

import (
	"context"
	"errors"
	"time"

	"weave/internal/actions"
	"weave/internal/agents"
	actionv1 "weave/proto/gen/action/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

// actionRunner is the part of the action service the handlers drive. The handlers translate
// and nothing more. What an action is and what it is told lives behind this.
type actionRunner interface {
	ListActions() ([]actions.Action, error)
	RunAction(ctx context.Context, agentID, name string) error
	EditAction(ctx context.Context, agentID, name string) error
}

// Service implements actionv1connect.ActionServiceHandler.
type Service struct {
	actions actionRunner
}

// NewService constructs the handler from the service that owns the rules.
func NewService(manager actionRunner) *Service {
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
			Name:           action.Name,
			Executable:     action.Executable,
			Description:    action.Header.Description,
			Timeout:        declaredTimeout(action.Header.Timeout),
			HeaderProblems: action.Header.Problems,
			Text:           action.Text,
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
	case errors.Is(err, agents.ErrAgentNotFound), errors.Is(err, actions.ErrActionNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, actions.ErrNotExecutable), errors.Is(err, actions.ErrNotStartable),
		errors.Is(err, actions.ErrDirMissing):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// EditAction opens an action's file in the user's editor and answers as soon as that process
// exists, since what happens in the pane afterwards belongs to whoever is reading it.
//
// It refuses what RunAction refuses, for the same reasons, plus FailedPrecondition for a
// compiled program, which is there and readable and simply has nothing to edit.
func (s *Service) EditAction(ctx context.Context, req *connect.Request[actionv1.EditActionRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.actions.EditAction(ctx, req.Msg.GetAgentId(), req.Msg.GetAction())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound), errors.Is(err, actions.ErrActionNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrNotEditable), errors.Is(err, actions.ErrNotExecutable),
		errors.Is(err, actions.ErrNotStartable), errors.Is(err, actions.ErrDirMissing):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// declaredTimeout is the timeout an action declared, and nothing for one that declared none.
// Zero on the wire would read as a run with no time at all rather than as a file that said
// nothing.
func declaredTimeout(timeout time.Duration) *durationpb.Duration {
	if timeout <= 0 {
		return nil
	}

	return durationpb.New(timeout)
}
