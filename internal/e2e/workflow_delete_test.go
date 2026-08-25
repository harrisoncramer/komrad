package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteWorkflowFinishesOnceCleanupCatchesUp covers a workflow whose one agent has a
// cleanup action that succeeds but takes real time to run: a cleanup action is always
// asynchronous, so the workflow's own agents are not gone by the time DeleteWorkflow's
// request returns, and only checking once, right then, would leave the workflow archived
// forever even though nothing failed. The delete must finish on its own once the cleanup
// action catches up, without a second press.
func TestDeleteWorkflowFinishesOnceCleanupCatchesUp(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	const teardown = "teardown"
	require.NoError(t, os.WriteFile(filepath.Join(instance.GetActionsDir, teardown), []byte(
		"#!/bin/sh\nsleep 0.2\nexit 0\n",
	), 0o755))

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:          "backend",
		Cwd:           t.TempDir(),
		CleanupAction: teardown,
	}))
	require.NoError(t, err)
	workflowID := workflow.Msg.GetWorkflow().GetId()

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflowID,
	}))
	require.NoError(t, err)

	_, err = instance.Workflows.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: workflowID}))
	require.NoError(t, err)

	_, err = instance.Workflows.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: workflowID}))
	require.NoError(t, err)

	got, err := instance.Stores.Workflows.GetIncludingArchived(ctx, workflowID)
	require.NoError(t, err)
	assert.NotNil(t, got, "the workflow is not gone yet, since the cleanup action has not finished")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		agent, err := instance.Stores.Agents.GetByID(ctx, created.Msg.GetAgent().GetId())
		assert.NoError(c, err)
		assert.Nil(c, agent, "its agent is gone once the cleanup action finishes")

		workflow, err := instance.Stores.Workflows.GetIncludingArchived(ctx, workflowID)
		assert.NoError(c, err)
		assert.Nil(c, workflow, "and the workflow it emptied out goes with it, with no second delete")
	}, 5*time.Second, 20*time.Millisecond)
}
