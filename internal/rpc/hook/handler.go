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
	HookTimeout() (time.Duration, error)
}

type Handler struct {
	hooks hookLister
}

// NewHandler constructs the handler from the services that own the rules.
func NewHandler(hooks hookLister) *Handler {
	return &Handler{hooks: hooks}
}

// ListHooks returns every file in the user's hooks directory, and a directory that is not
// there is an empty listing rather than an error.
func (s *Handler) ListHooks(_ context.Context, _ *emptypb.Empty) (*hookv1.ListHooksResponse, *fail.Error) {
	listed, err := s.hooks.ListHooks()
	if err != nil {
		return nil, rpcerr.From(err, "failed to list hooks")
	}

	limit, err := s.hooks.HookTimeout()
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the hook timeout")
	}

	resources := xslice.Map(listed, func(hook agents.Script) *hookv1.Hook {
		return &hookv1.Hook{
			Name:           hook.Name,
			Executable:     hook.Executable,
			Description:    hook.Header.Description,
			Timeout:        durationpb.New(hook.Header.TimeoutWithin(limit)),
			HeaderProblems: hook.Header.Problems,
		}
	})

	return &hookv1.ListHooksResponse{Hooks: resources}, nil
}
