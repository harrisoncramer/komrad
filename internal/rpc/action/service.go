package action

import (
	"context"
	"errors"
	"time"

	"weave/internal/agents"
	actionv1 "weave/proto/gen/action/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type actionRunner interface {
	ListActions() ([]agents.Action, error)
	RunAction(ctx context.Context, agentID, name string) error
	EditAction(ctx context.Context, agentID, name string) error
}

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
// there and one chmod away from running, and so are an agent whose directory has been removed
// from under it and the same action already running for the same agent, since none of those
// is a bug in weave.
func (s *Service) RunAction(ctx context.Context, req *connect.Request[actionv1.RunActionRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.actions.RunAction(ctx, req.Msg.GetAgentId(), req.Msg.GetAction())
	switch {
	case errors.Is(err, agents.ErrAgentNotFound), errors.Is(err, agents.ErrActionNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrActionNotExecutable), errors.Is(err, agents.ErrActionNotStartable),
		errors.Is(err, agents.ErrActionDirMissing), errors.Is(err, agents.ErrActionAlreadyRunning):
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
	case errors.Is(err, agents.ErrAgentNotFound), errors.Is(err, agents.ErrActionNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrNotEditable), errors.Is(err, agents.ErrActionNotExecutable),
		errors.Is(err, agents.ErrActionNotStartable), errors.Is(err, agents.ErrActionDirMissing),
		errors.Is(err, agents.ErrActionAlreadyRunning):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func declaredTimeout(timeout time.Duration) *durationpb.Duration {
	if timeout <= 0 {
		return nil
	}

	return durationpb.New(timeout)
}
