//go:build integration

package e2e

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/engine"
	"komrad/internal/komradtest"
	agentv1 "komrad/proto/gen/agent/v1"
	terminalv1 "komrad/proto/gen/terminal/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationTerminalAttachesToAnAgent covers a person attaching to an agent's terminal over
// the api, being bumped by a second attach, coming back, and leaving no view behind.
func TestIntegrationTerminalAttachesToAnAgent(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	agent := created.Msg.GetAgent()
	instance.WaitForHarness(agent.GetId())
	pane := instance.GetOwnedPane(agent.GetId())
	require.NotEmpty(t, pane)
	view := engine.ViewSession(pane)

	first := openAttach(t, instance, agent.GetId())
	t.Run("an attach shows the agent's pane", func(t *testing.T) {
		first.waitFor(t, "fake:")
	})

	t.Run("a second attach bumps the first and keeps the agent's session", func(t *testing.T) {
		second := openAttach(t, instance, agent.GetId())
		second.waitFor(t, "fake:")

		first.waitForExit(t)
		assert.Contains(t, listSessions(instance), view)
		assert.Equal(t, pane, instance.GetOwnedPane(agent.GetId()))

		second.cancel()
	})

	t.Run("an attach after the last one dropped resumes the agent's pane", func(t *testing.T) {
		again := openAttach(t, instance, agent.GetId())
		again.waitFor(t, "fake:")

		again.cancel()
	})

	t.Run("the view is gone once the last attach has ended", func(t *testing.T) {
		deadline := time.Now().Add(5 * time.Second)
		for slices.Contains(listSessions(instance), view) {
			require.True(t, time.Now().Before(deadline), "view %s outlived its last attach", view)
			time.Sleep(50 * time.Millisecond)
		}

		assert.Equal(t, pane, instance.GetOwnedPane(agent.GetId()))
	})
}

type attachStream struct {
	stream *connect.BidiStreamForClient[terminalv1.AttachRequest, terminalv1.AttachResponse]
	cancel context.CancelFunc
	output strings.Builder
}

// openAttach attaches to an agent and waits for the engine to say it is ready.
func openAttach(t *testing.T, instance *komradtest.Instance, agentID string) *attachStream {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)

	stream := instance.Terminals.Attach(ctx)
	t.Cleanup(func() { _ = stream.CloseResponse() })

	require.NoError(t, stream.Send(&terminalv1.AttachRequest{Message: &terminalv1.AttachRequest_Open{Open: &terminalv1.OpenAttach{
		AgentId: agentID,
		Size:    &terminalv1.Size{Rows: 24, Cols: 80},
		Term:    "xterm-256color",
	}}}))

	ready, err := stream.Receive()
	require.NoError(t, err)
	require.NotNil(t, ready.GetReady())

	return &attachStream{stream: stream, cancel: cancel}
}

func (a *attachStream) waitFor(t *testing.T, want string) {
	t.Helper()

	for !strings.Contains(a.output.String(), want) {
		response, err := a.stream.Receive()
		require.NoError(t, err, "the attach ended before showing %q, having shown %q", want, a.output.String())
		require.Nil(t, response.GetExit(), "the attach exited before showing %q", want)

		a.output.Write(response.GetOutput())
	}
}

func (a *attachStream) waitForExit(t *testing.T) {
	t.Helper()

	for {
		response, err := a.stream.Receive()
		require.NoError(t, err)

		if response.GetExit() != nil {
			return
		}
	}
}

func listSessions(instance *komradtest.Instance) []string {
	return strings.Split(instance.Tmux.Run("list-sessions", "-F", "#{session_name}"), "\n")
}
