package hook

import (
	"errors"
	"testing"
	"time"

	"weave/internal/agents"
	hookv1 "weave/proto/gen/hook/v1"
	hookv1weave "weave/proto/gen/hook/v1/hookv1weave"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeHookLister struct {
	hooks []agents.Script
	err   error
}

// ListHooks answers hooks unless err refuses the listing.
func (f fakeHookLister) ListHooks() ([]agents.Script, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.hooks, nil
}

// TestListHooks pins the code a failed listing reaches the caller as and how a listing renders.
func TestListHooks(t *testing.T) {
	tests := []struct {
		name     string
		lister   fakeHookLister
		wantCode connect.Code
		want     *hookv1.ListHooksResponse
	}{
		{
			name:     "a directory that cannot be read",
			lister:   fakeHookLister{err: errors.New("permission denied")},
			wantCode: connect.CodeInternal,
		},
		{
			name: "a hook with a header and one without",
			lister: fakeHookLister{hooks: []agents.Script{
				{
					Name:       "install-deps",
					Path:       "/hooks/install-deps",
					Executable: true,
					Header: agents.ScriptHeader{
						Description: "Install what the worktree needs before the agent starts.",
						Timeout:     45 * time.Second,
						Problems:    []string{"line 4: nope is not a directive weave knows"},
					},
				},
				{
					Name: "unrunnable",
					Path: "/hooks/unrunnable",
				},
			}},
			want: &hookv1.ListHooksResponse{Hooks: []*hookv1.Hook{
				{
					Name:           "install-deps",
					Executable:     true,
					Description:    "Install what the worktree needs before the agent starts.",
					Timeout:        durationpb.New(45 * time.Second),
					HeaderProblems: []string{"line 4: nope is not a directive weave knows"},
				},
				{
					Name: "unrunnable",
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := hookv1weave.HookServiceAdapter{Impl: NewHandler(tt.lister)}

			resp, err := service.ListHooks(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			if tt.want == nil {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))

				return
			}

			require.NoError(t, err)
			assert.Empty(t, cmp.Diff(tt.want, resp.Msg, protocmp.Transform()))
		})
	}
}
