package e2e

import (
	"testing"

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
