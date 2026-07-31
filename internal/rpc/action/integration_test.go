package action

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/actions"
	"weave/internal/agents"
	"weave/internal/appconfig"
	"weave/internal/interceptors"
	actionv1 "weave/proto/gen/action/v1"
	"weave/proto/gen/action/v1/actionv1connect"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestActionServiceOverHTTP drives the whole path a caller takes, over the wire, with the real
// runner starting a real script. Everything else here stops at a seam, and the two things only
// this can reach are the validation interceptor, which is what refuses a name before any handler
// sees it, and the payload actually arriving in a process.
func TestActionServiceOverHTTP(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	out := filepath.Join(work, "payload.json")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "record"), []byte(
		"#!/bin/sh\nprintf '%s' \"$WEAVE_AGENT\" > payload.json\n",
	), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))

	client := actionClient(t, dir, work)

	t.Run("a listing carries each file and the start of its text", func(t *testing.T) {
		resp, err := client.ListActions(context.Background(), connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)

		listed := resp.Msg.GetActions()
		require.Len(t, listed, 2)
		assert.Equal(t, "record", listed[0].GetName())
		assert.True(t, listed[0].GetExecutable())
		assert.Contains(t, string(listed[0].GetPreview()), "WEAVE_AGENT")
		assert.False(t, listed[1].GetExecutable(), "the file waiting on a chmod is listed too")
	})

	t.Run("a name reaching out of the directory is refused before the handler", func(t *testing.T) {
		_, err := client.RunAction(context.Background(), connect.NewRequest(&actionv1.RunActionRequest{
			AgentId: "a1",
			Action:  "../../../../bin/echo",
		}))

		require.Error(t, err)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("an action nobody wrote is not found", func(t *testing.T) {
		_, err := client.RunAction(context.Background(), connect.NewRequest(&actionv1.RunActionRequest{
			AgentId: "a1",
			Action:  "never-written",
		}))

		require.Error(t, err)
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("a file without the executable bit says which chmod fixes it", func(t *testing.T) {
		_, err := client.RunAction(context.Background(), connect.NewRequest(&actionv1.RunActionRequest{
			AgentId: "a1",
			Action:  "unrunnable",
		}))

		require.Error(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	})

	t.Run("firing starts the process and hands it the agent", func(t *testing.T) {
		_, err := client.RunAction(context.Background(), connect.NewRequest(&actionv1.RunActionRequest{
			AgentId: "a1",
			Action:  "record",
		}))
		require.NoError(t, err)

		// Firing returns as soon as the process exists rather than once it has finished, so
		// waiting for what it left behind is the only thing a caller can do.
		var written []byte
		require.Eventually(t, func() bool {
			read, err := os.ReadFile(out)
			written = read

			return err == nil && len(read) > 0
		}, 5*time.Second, 10*time.Millisecond, "the action never wrote %s", out)

		assert.Contains(t, string(written), `"name":"auth"`)
		assert.Contains(t, string(written), `"workflow":"backend"`)
		assert.Contains(t, string(written), `"pane":"%9"`)
	})
}

// actionClient stands the service up behind a real server, with the validation interceptor the
// panel and the MCP tools both go through, and returns a client pointed at it. work is the
// directory the agent claims, which is where an action it fires starts.
func actionClient(t *testing.T, dir, work string) actionv1connect.ActionServiceClient {
	t.Helper()

	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	service := agents.NewActionService(agents.ActionParams{
		Agents:    stubAgents{cwd: work},
		Workflows: stubWorkflows{},
		Tmux:      stubPanes{},
		Runner:    actions.Runner{},
		Load: func() (*appconfig.Config, error) {
			cfg := appconfig.Defaults()
			cfg.Actions.Dir = dir

			return cfg, nil
		},
		Addr:   "127.0.0.1:8787",
		Socket: "weave",
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

// stubAgents answers for one agent, so this needs no database.
type stubAgents struct {
	cwd string
}

func (s stubAgents) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if id != "a1" {
		return nil, nil
	}

	return &agentstore.Agent{
		ID: "a1", Name: "auth", State: agentstore.StateWaiting, Cwd: s.cwd,
		Harness: "claude", Pane: "%9", WorkflowID: "w1",
	}, nil
}

type stubWorkflows struct{}

func (stubWorkflows) Get(_ context.Context, id string) (*workflowstore.Workflow, error) {
	return &workflowstore.Workflow{ID: id, Name: "backend"}, nil
}

type stubPanes struct{}

func (stubPanes) PaneOwner(string) string { return "a1" }
