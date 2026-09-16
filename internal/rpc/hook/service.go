// Package hook serves the hooks a person has written.
package hook

import (
	"context"
	"time"

	"weave/internal/agents"
	"weave/internal/fail"
	"weave/internal/rpcerr"
	hookv1 "weave/proto/gen/hook/v1"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type hookLister interface {
	ListHooks() ([]agents.Script, error)
}

type Service struct {
	hooks hookLister
}

// NewService constructs the handler from the services that own the rules.
func NewService(hooks hookLister) *Service {
	return &Service{hooks: hooks}
}

// ListHooks returns every file in the user's hooks directory, and a directory that is not
// there is an empty listing rather than an error.
func (s *Service) ListHooks(_ context.Context, _ *emptypb.Empty) (*hookv1.ListHooksResponse, *fail.Error) {
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

func declaredTimeout(timeout time.Duration) *durationpb.Duration {
	if timeout <= 0 {
		return nil
	}

	return durationpb.New(timeout)
}
