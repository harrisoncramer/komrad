// Package hook serves the hooks a person has written.
package hook

import (
	"context"
	"time"

	"komrad/internal/agents"
	"komrad/internal/fail"
	"komrad/internal/rpcerr"
	"komrad/lib/xslice"
	hookv1 "komrad/proto/gen/hook/v1"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type hookLister interface {
	ListHooks() ([]agents.Script, error)
}

type Handler struct {
	hooks hookLister
}

// NewHandler constructs the handler from the services that own the rules.
func NewHandler(hooks hookLister) *Handler {
	return &Handler{hooks: hooks}
}

// ListHooks returns every hook komrad ships and every file in the user's hooks directory.
func (s *Handler) ListHooks(_ context.Context, _ *emptypb.Empty) (*hookv1.ListHooksResponse, *fail.Error) {
	listed, err := s.hooks.ListHooks()
	if err != nil {
		return nil, rpcerr.From(err, "failed to list hooks")
	}

	resources := xslice.Map(listed, func(hook agents.Script) *hookv1.Hook {
		return &hookv1.Hook{
			Name:           hook.Name,
			Executable:     hook.Executable,
			Description:    hook.Header.Description,
			Timeout:        declaredTimeout(hook.Header.Timeout),
			HeaderProblems: hook.Header.Problems,
		}
	})

	return &hookv1.ListHooksResponse{Hooks: resources}, nil
}

func declaredTimeout(timeout time.Duration) *durationpb.Duration {
	if timeout <= 0 {
		return nil
	}

	return durationpb.New(timeout)
}
