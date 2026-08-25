package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestEmptyArchiveDestroysTheWholeArchive is the panel's emptying gesture end to end. The
// request only queues the sweep, so what the keypress promises holds or fails in the job
// behind it: every archived agent gone, their panes with them, and the agents still running
// left alone.
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
// a cleanup action that will not run: the row cannot be destroyed, so it has to be somewhere,
// and the archive the user was emptying is the only place they would think to look for it.
// Taking it back to the live list instead would report an archive emptied of rows nothing
// destroyed, filed where nobody asked.
func TestEmptyArchiveLeavesAFailedTeardownInTheArchive(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	const teardown = "teardown"
	require.NoError(t, os.WriteFile(filepath.Join(instance.GetActionsDir, teardown), []byte(
		"#!/bin/sh\necho boom >&2\nexit 1\n",
	), 0o755))

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:          "backend",
		Cwd:           t.TempDir(),
		CleanupAction: teardown,
	}))
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
}
