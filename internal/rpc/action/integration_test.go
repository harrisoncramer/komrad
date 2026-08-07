package action

import (
	"context"
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
	// Where the agent claims to have worked, and never created. An action fired on an agent
	// whose worktree has already been removed has to run anyway, since tidying up after that
	// removal is what one is for.
	work := filepath.Join(t.TempDir(), "removed-worktree")
	out := filepath.Join(dir, "payload.json")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "record"), []byte(
		"#!/bin/sh\nprintf '%s' \"$WEAVE_AGENT\" > payload.json\n",
	), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))
	// The editing action the panel's editor key reaches, written here rather than shipped, so
	// this also covers a user's own file standing in for the built-in one.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_edit_action"), []byte(
		"#!/bin/sh\nprintf '%s' \"$WEAVE_EDIT_PATH\" > edited.txt\n",
	), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compiled"), []byte{0x7f, 'E', 'L', 'F', 0x00}, 0o755))

	client := actionClient(t, dir, work)

	t.Run("a listing carries each file and what it declares", func(t *testing.T) {
		resp, err := client.ListActions(context.Background(), connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)

		listed := resp.Msg.GetActions()
		require.Len(t, listed, 4)
		assert.Equal(t, []string{"_edit_action", "compiled", "record", "unrunnable"}, names(listed))
		assert.True(t, listed[2].GetExecutable())
		assert.True(t, listed[2].GetText())
		assert.False(t, listed[1].GetText(), "a compiled program is listed as what it is")
		assert.False(t, listed[3].GetExecutable(), "the file waiting on a chmod is listed too")
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
		// The relative write landing here is the whole of the claim: the process started
		// among its own siblings, which is what lets one action source another, and it
		// started at all despite the agent's own directory being gone.
		assert.Contains(t, string(written), `"cwd":"`+work+`"`)
	})

	t.Run("editing hands the editing action the path the name resolved to", func(t *testing.T) {
		_, err := client.EditAction(context.Background(), connect.NewRequest(&actionv1.EditActionRequest{
			AgentId: "a1",
			Action:  "record",
		}))
		require.NoError(t, err)

		edited := filepath.Join(dir, "edited.txt")

		var written []byte
		require.Eventually(t, func() bool {
			read, err := os.ReadFile(edited)
			written = read

			return err == nil && len(read) > 0
		}, 5*time.Second, 10*time.Millisecond, "the editing action never wrote %s", edited)

		assert.Equal(t, filepath.Join(dir, "record"), string(written))
	})

	t.Run("a compiled program is refused rather than opened", func(t *testing.T) {
		_, err := client.EditAction(context.Background(), connect.NewRequest(&actionv1.EditActionRequest{
			AgentId: "a1",
			Action:  "compiled",
		}))

		require.Error(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	})
}

// names is what a listing holds, in the order it arrived.
func names(listed []*actionv1.Action) []string {
	found := make([]string, 0, len(listed))
	for _, action := range listed {
		found = append(found, action.GetName())
	}

	return found
}

// actionClient stands the service up behind a real server, with the validation interceptor the
// panel and the MCP tools both go through, and returns a client pointed at it. work is the
// directory the agent claims, which an action is told about and never runs in.
func actionClient(t *testing.T, dir, work string) actionv1connect.ActionServiceClient {
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
