//go:build integration

package e2e

import (
	"path/filepath"
	"testing"

	"komrad/internal/komradtest"
	agentv1 "komrad/proto/gen/agent/v1"
	terminalv1 "komrad/proto/gen/terminal/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationShellOpensInAnAgentsDirectory covers a person opening a shell on the engine in
// an agent's directory over the api, typing into it, and getting its exit status back, whether it
// exits on its own or because its input ran out.
func TestIntegrationShellOpensInAnAgentsDirectory(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  dir,
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	agent := created.Msg.GetAgent()

	t.Run("a shell in the agent's directory runs there and answers with its exit status", func(t *testing.T) {
		stream, err := openShell(t, instance, agent.GetCwd())
		require.NoError(t, err)

		require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Resize{Resize: &terminalv1.Size{Rows: 30, Cols: 100}}}))
		require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: []byte("pwd -P; stty size; exit 5\n")}}))

		var output []byte
		var exit *terminalv1.Exit
		for exit == nil {
			response, err := stream.Receive()
			require.NoError(t, err)

			output = append(output, response.GetOutput()...)
			exit = response.GetExit()
		}

		assert.Equal(t, int32(5), exit.GetCode())
		assert.Contains(t, string(output), dir+"\r\n")
		assert.Contains(t, string(output), "30 100\r\n")
	})

	t.Run("a shell whose input runs out exits once it has run what it was given", func(t *testing.T) {
		stream, err := openShell(t, instance, agent.GetCwd())
		require.NoError(t, err)

		require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Input{Input: []byte("echo pi\"\"ped\n")}}))
		require.NoError(t, stream.CloseRequest())

		var output []byte
		var exit *terminalv1.Exit
		for exit == nil {
			response, err := stream.Receive()
			require.NoError(t, err)

			output = append(output, response.GetOutput()...)
			exit = response.GetExit()
		}

		assert.Equal(t, int32(0), exit.GetCode())
		assert.Contains(t, string(output), "piped\r\n")
	})
}

// openShell opens a shell in dir and waits for the engine to say it is ready.
func openShell(t *testing.T, instance *komradtest.Instance, dir string) (*connect.BidiStreamForClient[terminalv1.ShellRequest, terminalv1.ShellResponse], error) {
	t.Helper()

	stream := instance.Terminals.Shell(t.Context())
	t.Cleanup(func() { _ = stream.CloseResponse() })

	require.NoError(t, stream.Send(&terminalv1.ShellRequest{Message: &terminalv1.ShellRequest_Open{Open: &terminalv1.OpenShell{
		Dir:  dir,
		Size: &terminalv1.Size{Rows: 24, Cols: 80},
	}}}))

	ready, err := stream.Receive()
	if err != nil {
		return nil, err
	}

	require.NotNil(t, ready.GetReady())

	return stream, nil
}
