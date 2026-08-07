package e2e

import (
	"testing"

	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorktreeRemovedReachesTheCaller walks the flag the whole way a person's screen does:
// the worktree syncer writes it to the row, and a listing over the wire is the only thing
// the panel ever sees. Nothing here builds a stored agent by hand, since a conversion that
// drops the flag is exactly what a hand-built one cannot catch.
func TestWorktreeRemovedReachesTheCaller(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
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

	agentID := created.Msg.GetAgent().GetId()
	assert.False(t, created.Msg.GetAgent().GetWorktreeRemoved(), "a freshly created agent has a cwd")

	require.NoError(t, instance.Stores.Agents.SetWorktreeRemovedByID(ctx, agentID, true))

	fetched, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agentID}))
	require.NoError(t, err)
	assert.True(t, fetched.Msg.GetAgent().GetWorktreeRemoved())

	listed, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{}))
	require.NoError(t, err)

	found := false
	for _, agent := range listed.Msg.GetAgents() {
		if agent.GetId() == agentID {
			found = true

			assert.True(t, agent.GetWorktreeRemoved())
		}
	}

	require.True(t, found, "the agent is on the live listing")
}
