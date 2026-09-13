//go:build integration

package e2e

import (
	"testing"
	"time"

	"weave/internal/store"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteWorkflowFinishesOnceCleanupCatchesUp covers a deprovisioner that takes real
// time, where checking only once would leave the workflow archived though nothing failed.
func TestDeleteWorkflowFinishesOnceCleanupCatchesUp(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	replaceWorktreeTeardown(t, instance, "#!/bin/sh\nsleep 0.2\nexit 0\n")

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(worktreeWorkflow(t)))
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
	assert.NotNil(t, got, "the workflow is not gone yet, since the deprovisioner has not finished")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := instance.Stores.Agents.GetByID(ctx, created.Msg.GetAgent().GetId())
		assert.ErrorIs(c, err, store.ErrNotFound, "its agent is gone once the deprovisioner finishes")

		_, err = instance.Stores.Workflows.GetIncludingArchived(ctx, workflowID)
		assert.ErrorIs(c, err, store.ErrNotFound, "and the workflow it emptied out goes with it, with no second delete")
	}, 5*time.Second, 20*time.Millisecond)
}
