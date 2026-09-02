package action

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/config"
	"weave/internal/actions"
	"weave/internal/agents"
	"weave/internal/interceptors"
	"weave/internal/store"
	actionv1 "weave/proto/gen/action/v1"
	"weave/proto/gen/action/v1/actionv1connect"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestActionServiceOverHTTP drives the whole path over the wire with real scripts, which is the
// only way to reach the validation interceptor and the payload arriving in a process.
func TestActionServiceOverHTTP(t *testing.T) {
	hooks := t.TempDir()
	builtins := t.TempDir()

	work := filepath.Join(t.TempDir(), "removed-worktree")
	out := filepath.Join(builtins, "payload.json")

	require.NoError(t, os.WriteFile(filepath.Join(hooks, "install-deps"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "compiled"), []byte{0x7f, 'E', 'L', 'F', 0x00}, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(builtins, "_open_pr"), []byte(
		"#!/bin/sh\nprintf '%s' \"$WEAVE_AGENT\" > payload.json\n",
	), 0o755))

	client := actionClient(t, hooks, builtins, work)

	t.Run("a listing carries each hook and what it declares", func(t *testing.T) {
		resp, err := client.ListHooks(context.Background(), connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)

		listed := resp.Msg.GetHooks()
		require.Len(t, listed, 3)
		assert.Equal(t, []string{"compiled", "install-deps", "unrunnable"}, names(listed))
		assert.True(t, listed[1].GetExecutable())
		assert.False(t, listed[2].GetExecutable(), "the file waiting on a chmod is listed too")
	})

	t.Run("an agent nobody has is not found", func(t *testing.T) {
		_, err := client.OpenPullRequest(context.Background(), connect.NewRequest(&actionv1.OpenPullRequestRequest{
			AgentId: "gone",
		}))

		require.Error(t, err)
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("an agent id nothing could answer to is refused before the handler", func(t *testing.T) {
		_, err := client.OpenPullRequest(context.Background(), connect.NewRequest(&actionv1.OpenPullRequestRequest{
			AgentId: "",
		}))

		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("opening starts the built-in and hands it the agent", func(t *testing.T) {
		_, err := client.OpenPullRequest(context.Background(), connect.NewRequest(&actionv1.OpenPullRequestRequest{
			AgentId: "a1",
		}))
		require.NoError(t, err)

		var written []byte
		require.Eventually(t, func() bool {
			read, err := os.ReadFile(out)
			written = read

			return err == nil && len(read) > 0
		}, 5*time.Second, 10*time.Millisecond, "the built-in never wrote %s", out)

		assert.Contains(t, string(written), `"name":"auth"`)
		assert.Contains(t, string(written), `"workflow":"backend"`)
		assert.Contains(t, string(written), `"pane":"%9"`)

		assert.Contains(t, string(written), `"cwd":"`+work+`"`)
	})
}

func names(listed []*actionv1.Hook) []string {
	found := make([]string, 0, len(listed))
	for _, hook := range listed {
		found = append(found, hook.GetName())
	}

	return found
}

func actionClient(t *testing.T, hooks, builtins, work string) actionv1connect.ActionServiceClient {
	t.Helper()

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	service := agents.NewActionService(agents.ActionParams{
		Agents:    stubAgents{cwd: work},
		Workflows: stubWorkflows{},
		Tmux:      stubPanes{},
		Runner:    actions.NewRunner(),
		Load: func() (*config.Config, error) {
			cfg := config.Defaults()
			cfg.Hooks.Dir = hooks

			return cfg, nil
		},
		BuiltinDir: builtins,
		Addr:       "127.0.0.1:8787",
		Socket:     "weave",
	})

	mux := http.NewServeMux()
	mux.Handle(actionv1connect.NewActionServiceHandler(
		NewService(service),
		connect.WithInterceptors(validation),
	))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return actionv1connect.NewActionServiceClient(server.Client(), server.URL)
}

type stubAgents struct {
	cwd string
}

func (s stubAgents) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if id != "a1" {
		return nil, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	return &agentstore.Agent{
		ID: "a1", Name: "auth", State: agentstore.StateWaiting, Cwd: s.cwd,
		Harness: "claude", Pane: "%9", WorkflowID: "w1",
	}, nil
}

type stubWorkflows struct{}

func (s stubWorkflows) Get(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	return s.GetIncludingArchived(ctx, id)
}

func (stubWorkflows) GetIncludingArchived(_ context.Context, id string) (*workflowstore.Workflow, error) {
	return &workflowstore.Workflow{ID: id, Name: "backend"}, nil
}

type stubPanes struct{}

func (stubPanes) GetPaneOwner(string) string            { return "a1" }
func (stubPanes) GetHostSocket() string                 { return "" }
func (stubPanes) ShowGlobal(string) string              { return "" }
func (stubPanes) GetInfoPane() string                   { return "" }
func (stubPanes) DefaultShell() string                  { return "/bin/sh" }
func (stubPanes) IsPaneAlive(string) bool               { return true }
func (stubPanes) RespawnPane(string, string, ...string) {}
func (stubPanes) WaitFor(context.Context, string) error { return nil }
