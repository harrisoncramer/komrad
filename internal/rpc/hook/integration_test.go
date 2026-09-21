package hook

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"weave/lib/xslice"
	hookv1weave "weave/proto/gen/hook/v1/hookv1weave"

	"weave/config"
	"weave/internal/interceptors"
	"weave/internal/scriptrun"
	"weave/internal/scripts"
	"weave/internal/store"
	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestHookServiceOverHTTP drives the whole path over the wire with real scripts.
func TestHookServiceOverHTTP(t *testing.T) {
	hooks := t.TempDir()
	builtins := t.TempDir()

	work := filepath.Join(t.TempDir(), "removed-worktree")

	require.NoError(t, os.WriteFile(filepath.Join(hooks, "install-deps"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "compiled"), []byte{0x7f, 'E', 'L', 'F', 0x00}, 0o755))

	client := hookClient(t, hookClientParams{
		hooks:    hooks,
		builtins: builtins,
		work:     work,
	})

	t.Run("a listing carries each hook and what it declares", func(t *testing.T) {
		resp, err := client.ListHooks(t.Context(), connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)

		listed := resp.Msg.GetHooks()
		require.Len(t, listed, 3)
		assert.Equal(t, []string{"compiled", "install-deps", "unrunnable"}, names(listed))
		assert.True(t, listed[1].GetExecutable())
		assert.False(t, listed[2].GetExecutable(), "the file waiting on a chmod is listed too")
	})
}

func names(listed []*hookv1.Hook) []string {
	found := xslice.Map(listed, func(hook *hookv1.Hook) string {
		return hook.GetName()
	})

	return found
}

type hookClientParams struct {
	hooks    string
	builtins string
	work     string
}

func hookClient(t *testing.T, p hookClientParams) hookv1connect.HookServiceClient {
	t.Helper()

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	service := scriptrun.NewScriptService(scriptrun.NewScriptServiceParams{
		Agents:    fakeAgents{cwd: p.work},
		Workflows: fakeWorkflows{},
		Tmux:      fakePanes{},
		Runner:    scripts.NewRunner(),
		Load: func() (*config.Config, error) {
			cfg := config.Defaults()
			cfg.Hooks.Dir = p.hooks

			return cfg, nil
		},
		BuiltinDir: p.builtins,
		Addr:       "127.0.0.1:8787",
		Socket:     "weave",
	})

	mux := http.NewServeMux()
	mux.Handle(hookv1connect.NewHookServiceHandler(
		hookv1weave.HookServiceAdapter{Impl: NewHandler(service)},
		connect.WithInterceptors(validation),
	))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return hookv1connect.NewHookServiceClient(server.Client(), server.URL)
}

type fakeAgents struct {
	cwd string
}

func (s fakeAgents) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if id != "a1" && id != "a2" {
		return nil, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	agent := &agentstore.Agent{
		ID: id, Name: "auth", State: agentstore.StateWaiting, Cwd: s.cwd,
		Harness: "claude", Pane: "%9", WorkflowID: "w1",
	}
	if id == "a1" {
		agent.Metadata = []agentstore.Metadata{
			{Key: "pr", Value: "https://github.com/weave/weave/pull/1"},
		}
	}

	return agent, nil
}

type fakeWorkflows struct{}

func (s fakeWorkflows) Get(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	return s.GetIncludingArchived(ctx, id)
}

func (fakeWorkflows) GetIncludingArchived(_ context.Context, id string) (*workflowstore.Workflow, error) {
	return &workflowstore.Workflow{ID: id, Name: "backend"}, nil
}

type fakePanes struct{}

func (fakePanes) GetPaneOwner(string) string { return "a1" }
func (fakePanes) GetHostSocket() string      { return "" }
func (fakePanes) GetInfoPane() string        { return "" }
func (fakePanes) GetDefaultShell() string    { return "/bin/sh" }
