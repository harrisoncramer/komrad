//go:build integration

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationOrchestrator walks a workflow's orchestrator from its first start through the
// work it hands out to coming back out of the archive.
func TestIntegrationOrchestrator(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflowID := createWorkflow(t, instance, "checkout")
	otherID := createWorkflow(t, instance, "docs")

	var orchestrator *agentv1.Agent

	t.Run("a workflow that is not there is refused", func(t *testing.T) {
		_, err := instance.Agents.StartOrchestrator(ctx, connect.NewRequest(&agentv1.StartOrchestratorRequest{
			WorkflowId: "01a0d1db-0000-7000-8000-000000000000",
		}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("a workflow that never had one gets one of its own", func(t *testing.T) {
		orchestrator = startOrchestrator(t, instance, workflowID)

		assert.Equal(t, agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR, orchestrator.GetRole())
		assert.Equal(t, workflowID, orchestrator.GetWorkflowId())
		assert.Equal(t, workflowID, filepath.Base(orchestrator.GetCwd()), "it runs in a scratch directory named for its workflow")
		assert.Empty(t, orchestrator.GetBranch(), "it is given no worktree")

		instance.WaitForHarness(orchestrator.GetId())
	})

	t.Run("asking again returns the one already running", func(t *testing.T) {
		again := startOrchestrator(t, instance, workflowID)

		assert.Equal(t, orchestrator.GetId(), again.GetId())
	})

	t.Run("it spawns work into its own workflow", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:     "write the migration",
			Title:      "migrations",
			WorkflowId: workflowID,
		})
		callAs(req, orchestrator.GetId())

		created, err := instance.Agents.CreateAgent(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER, created.Msg.GetAgent().GetRole())
	})

	t.Run("it cannot spawn into another workflow", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:     "rewrite the readme",
			Title:      "readme",
			WorkflowId: otherID,
		})
		callAs(req, orchestrator.GetId())

		_, err := instance.Agents.CreateAgent(ctx, req)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("it cannot be given a child", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:        "read the plan",
			Title:         "reader",
			WorkflowId:    workflowID,
			ParentAgentId: &orchestrator.Id,
		})
		callAs(req, orchestrator.GetId())

		_, err := instance.Agents.CreateAgent(ctx, req)
		assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("one that held a conversation comes back out of the archive", func(t *testing.T) {
		archiveAgent(t, instance, orchestrator.GetId())

		restored := startOrchestrator(t, instance, workflowID)
		assert.Equal(t, orchestrator.GetId(), restored.GetId())
		assert.Empty(t, restored.GetArchivedAt())

		instance.WaitForHarness(restored.GetId())
	})

	t.Run("one whose CLI never wrote a conversation is replaced by a fresh one", func(t *testing.T) {
		silent := startOrchestrator(t, instance, otherID)
		instance.WaitForHarness(silent.GetId())
		archiveAgent(t, instance, silent.GetId())

		stored, err := instance.Stores.Agents.GetByID(ctx, silent.GetId())
		require.NoError(t, err)
		require.NoError(t, os.Remove(instance.Harness.LogPath(stored.SessionID)))

		fresh := startOrchestrator(t, instance, otherID)
		assert.NotEqual(t, silent.GetId(), fresh.GetId())

		_, err = instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: silent.GetId()}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "the one that never ran is gone")
	})
}

func createWorkflow(t *testing.T, instance *weavetest.Instance, name string) string {
	t.Helper()

	created, err := instance.Workflows.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: name,
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	return created.Msg.GetWorkflow().GetId()
}

func startOrchestrator(t *testing.T, instance *weavetest.Instance, workflowID string) *agentv1.Agent {
	t.Helper()

	started, err := instance.Agents.StartOrchestrator(t.Context(), connect.NewRequest(&agentv1.StartOrchestratorRequest{
		WorkflowId: workflowID,
	}))
	require.NoError(t, err)

	return started.Msg.GetAgent()
}

func archiveAgent(t *testing.T, instance *weavetest.Instance, id string) {
	t.Helper()

	_, err := instance.Agents.ArchiveAgents(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentsRequest{
		Ids: []string{id},
	}))
	require.NoError(t, err)
}
