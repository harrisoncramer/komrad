// Package action serves what is left of the action RPCs: the hooks a person has written, and
// the built-in script behind the panel's open-pull-request key.
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

type hookRunner interface {
	ListHooks() ([]agents.Action, error)
	OpenPullRequest(ctx context.Context, agentID string) error
}

type Service struct {
	actions hookRunner
}

// NewService constructs the handler from the service that owns the rules.
func NewService(manager hookRunner) *Service {
	return &Service{actions: manager}
}

// ListHooks returns every file in the user's hooks directory, and a directory that is not
// there is an empty listing rather than an error.
func (s *Service) ListHooks(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[actionv1.ListHooksResponse], error) {
	listed, err := s.actions.ListHooks()
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list hooks")
	}

	resources := make([]*actionv1.Hook, 0, len(listed))
	for _, hook := range listed {
		resources = append(resources, &actionv1.Hook{
			Name:           hook.Name,
			Executable:     hook.Executable,
			Description:    hook.Header.Description,
			Timeout:        declaredTimeout(hook.Header.Timeout),
			HeaderProblems: hook.Header.Problems,
		})
	}

	return connect.NewResponse(&actionv1.ListHooksResponse{Hooks: resources}), nil
}

// OpenPullRequest fires the built-in script behind the panel's own key, which is the one script
// weave runs on demand rather than at a moment of its own choosing.
func (s *Service) OpenPullRequest(ctx context.Context, req *connect.Request[actionv1.OpenPullRequestRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.actions.OpenPullRequest(ctx, req.Msg.GetAgentId()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to open the pull request")
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func declaredTimeout(timeout time.Duration) *durationpb.Duration {
	if timeout <= 0 {
		return nil
	}

	return durationpb.New(timeout)
}
