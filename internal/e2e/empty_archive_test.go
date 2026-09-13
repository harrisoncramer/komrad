//go:build integration

package e2e

import (
	"testing"
	"time"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestEmptyArchiveDestroysTheWholeArchive covers the emptying gesture end to end, the request
// only queueing the sweep the keypress's promise holds in.
func TestEmptyArchiveDestroysTheWholeArchive(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	create := func(title string) *agentv1.Agent {
		t.Helper()

		created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:     "fix the flaky login test",
			Title:      title,
			WorkflowId: workflow.Msg.GetWorkflow().GetId(),
		}))
		require.NoError(t, err)

		return created.Msg.GetAgent()
	}

	putAway := []*agentv1.Agent{create("login"), create("tokens"), create("cache")}
	kept := create("running")

	ids := make([]string, 0, len(putAway))
	for _, agent := range putAway {
		ids = append(ids, agent.GetId())
	}

	_, err = instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: ids}))
	require.NoError(t, err)

	_, err = instance.Agents.EmptyArchive(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	instance.WorkQueue()

	archived, err := instance.Stores.Agents.ListPage(ctx, 100, nil, true)
	require.NoError(t, err)
	assert.Empty(t, archived, "the sweep leaves nothing in the archive")

	for _, agent := range putAway {
		assert.Empty(t, instance.GetOwnedPane(agent.GetId()), "a destroyed agent keeps no pane")
	}

	live, err := instance.Stores.Agents.ListPage(ctx, 100, nil, false)
	require.NoError(t, err)
	require.Len(t, live, 1, "the sweep is the archive's alone")
	assert.Equal(t, kept.GetId(), live[0].ID)
}

// TestEmptyArchiveLeavesAFailedTeardownInTheArchive covers what the gesture promises against
// a deprovisioner that will not run: the row cannot be destroyed, so it has to be somewhere,
// and the archive the user was emptying is the only place they would think to look for it.
func TestEmptyArchiveLeavesAFailedTeardownInTheArchive(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	replaceWorktreeTeardown(t, instance, "#!/bin/sh\necho boom >&2\nexit 1\n")

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(worktreeWorkflow(t)))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	id := created.Msg.GetAgent().GetId()

	_, err = instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{Ids: []string{id}}))
	require.NoError(t, err)

	_, err = instance.Agents.EmptyArchive(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	instance.WorkQueue()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		agent, err := instance.Stores.Agents.GetByID(ctx, id)
		assert.NoError(c, err)
		if !assert.NotNil(c, agent) {
			return
		}

		assert.Equal(c, agentstore.StateFailedToCleanup, agent.State)
		assert.NotNil(c, agent.ArchivedAt, "the row the sweep could not destroy is still in the archive")
	}, 10*time.Second, 10*time.Millisecond)

	live, err := instance.Stores.Agents.ListPage(ctx, 100, nil, false)
	require.NoError(t, err)
	assert.Empty(t, live, "a teardown that failed does not put the row back on the live list")

	stillArchived, err := instance.Stores.Workflows.GetIncludingArchived(ctx, workflow.Msg.GetWorkflow().GetId())
	require.NoError(t, err)
	assert.NotNil(t, stillArchived, "the workflow the straggler is still in stays archived rather than deleted")
}

// TestEmptyArchiveDeletesAWorkflowItEmptiesOut covers the other half of the sweep's promise:
// an archived workflow it leaves with nothing inside it does not sit there forever either.
func TestEmptyArchiveDeletesAWorkflowItEmptiesOut(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
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

	_, err = instance.Agents.EmptyArchive(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	instance.WorkQueue()

	_, err = instance.Stores.Agents.GetByID(ctx, created.Msg.GetAgent().GetId())
	require.ErrorIs(t, err, store.ErrNotFound, "its agent is gone along with the workflow")

	_, err = instance.Stores.Workflows.GetIncludingArchived(ctx, workflowID)
	require.ErrorIs(t, err, store.ErrNotFound, "the workflow the sweep emptied out is deleted too")
}
