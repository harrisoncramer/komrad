package hook

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	hookv1weave "weave/proto/gen/hook/v1/hookv1weave"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/interceptors"
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

// TestHookServiceOverHTTP drives the whole path over the wire with real scripts, which is the
// only way to reach the validation interceptor and the payload arriving in a process.
func TestHookServiceOverHTTP(t *testing.T) {
	hooks := t.TempDir()
	builtins := t.TempDir()

	work := filepath.Join(t.TempDir(), "removed-worktree")

	require.NoError(t, os.WriteFile(filepath.Join(hooks, "install-deps"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "compiled"), []byte{0x7f, 'E', 'L', 'F', 0x00}, 0o755))

	opened := &recordingOpener{}
	client := hookClient(t, hookClientParams{
		hooks:    hooks,
		builtins: builtins,
		work:     work,
		opener:   opened,
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

	t.Run("an agent nobody has is not found", func(t *testing.T) {
		_, err := client.OpenPullRequest(t.Context(), connect.NewRequest(&hookv1.OpenPullRequestRequest{
			AgentId: "gone",
		}))

		require.Error(t, err)
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("an agent id nothing could answer to is refused before the handler", func(t *testing.T) {
		_, err := client.OpenPullRequest(t.Context(), connect.NewRequest(&hookv1.OpenPullRequestRequest{
			AgentId: "",
		}))

		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("an agent with nothing recorded is refused", func(t *testing.T) {
		_, err := client.OpenPullRequest(t.Context(), connect.NewRequest(&hookv1.OpenPullRequestRequest{
			AgentId: "a2",
		}))

		require.Error(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		assert.Empty(t, opened.targets, "nothing was handed to the opener")
	})

	t.Run("opening hands the recorded URL to the opener", func(t *testing.T) {
		_, err := client.OpenPullRequest(t.Context(), connect.NewRequest(&hookv1.OpenPullRequestRequest{
			AgentId: "a1",
		}))
		require.NoError(t, err)

		assert.Equal(t, []string{"https://github.com/weave/weave/pull/1"}, opened.targets)
	})
}

type recordingOpener struct {
	targets []string
}

func (o *recordingOpener) OpenURL(_ context.Context, target string) error {
	o.targets = append(o.targets, target)

	return nil
}

func names(listed []*hookv1.Hook) []string {
	found := make([]string, 0, len(listed))
	for _, hook := range listed {
		found = append(found, hook.GetName())
	}

	return found
}

type hookClientParams struct {
	hooks    string
	builtins string
	work     string
	opener   *recordingOpener
}

func hookClient(t *testing.T, p hookClientParams) hookv1connect.HookServiceClient {
	t.Helper()

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	service := agents.NewScriptService(agents.NewScriptServiceParams{
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

	pullRequests := agents.NewPullRequestOpener(agents.NewPullRequestOpenerParams{
		Agents: fakeAgents{cwd: p.work},
		Opener: p.opener,
	})

	mux := http.NewServeMux()
	mux.Handle(hookv1connect.NewHookServiceHandler(
		hookv1weave.HookServiceAdapter{Impl: NewService(service, pullRequests)},
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
