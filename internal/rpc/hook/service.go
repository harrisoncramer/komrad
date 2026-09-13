// Package hook serves the hooks a person has written, and the panel's open-pull-request key.
package hook

import (
	"context"
	"time"

	"weave/internal/agents"
	"weave/internal/fail"
	"weave/internal/rpcerr"
	hookv1 "weave/proto/gen/hook/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type hookLister interface {
	ListHooks() ([]agents.Script, error)
}

type pullRequestOpener interface {
	OpenPullRequest(ctx context.Context, agentID string) error
}

type Service struct {
	hooks        hookLister
	pullRequests pullRequestOpener
}

// NewService constructs the handler from the services that own the rules.
func NewService(hooks hookLister, pullRequests pullRequestOpener) *Service {
	return &Service{
		hooks:        hooks,
		pullRequests: pullRequests,
	}
}

// ListHooks returns every file in the user's hooks directory, and a directory that is not
// there is an empty listing rather than an error.
func (s *Service) ListHooks(ctx context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[hookv1.ListHooksResponse], error) {
	return rpcerr.Answer(s.listHooks(ctx, req.Msg))
}

func (s *Service) listHooks(_ context.Context, _ *emptypb.Empty) (*hookv1.ListHooksResponse, *fail.Error) {
	listed, err := s.hooks.ListHooks()
	if err != nil {
		return nil, rpcerr.From(err, "failed to list hooks")
	}

	resources := make([]*hookv1.Hook, 0, len(listed))
	for _, hook := range listed {
		resources = append(resources, &hookv1.Hook{
			Name:           hook.Name,
			Executable:     hook.Executable,
			Description:    hook.Header.Description,
			Timeout:        declaredTimeout(hook.Header.Timeout),
			HeaderProblems: hook.Header.Problems,
		})
	}

	return &hookv1.ListHooksResponse{Hooks: resources}, nil
}

// OpenPullRequest opens the pull request an agent recorded, which is the panel's own key and
// the one thing weave does to an agent on demand rather than at a moment of its own choosing.
func (s *Service) OpenPullRequest(ctx context.Context, req *connect.Request[hookv1.OpenPullRequestRequest]) (*connect.Response[emptypb.Empty], error) {
	return rpcerr.Answer(s.openPullRequest(ctx, req.Msg))
}

func (s *Service) openPullRequest(ctx context.Context, req *hookv1.OpenPullRequestRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.pullRequests.OpenPullRequest(ctx, req.GetAgentId()); err != nil {
		return nil, rpcerr.From(err, "failed to open the pull request")
	}

	return &emptypb.Empty{}, nil
}

func declaredTimeout(timeout time.Duration) *durationpb.Duration {
	if timeout <= 0 {
		return nil
	}

	return durationpb.New(timeout)
}
