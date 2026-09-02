package e2e

import (
	"testing"

	"weave/internal/caller"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChildAgent walks a child from attached to archived through the API the panel and the MCP
// tools both call, since the rules it runs under are the whole point of the relation.
func TestChildAgent(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	dir := t.TempDir()
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "weave",
		Cwd:  dir,
	}))
	require.NoError(t, err)
	workflowID := workflow.Msg.GetWorkflow().GetId()

	parent := createAgent(t, instance, workflowID, "author")

	req := connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:        "read the change",
		Title:         "reviewer",
		WorkflowId:    workflowID,
		ParentAgentId: &parent.Id,
	})
	caller.Caller{AgentID: parent.GetId()}.SetHeader(req.Header())

	created, err := instance.Agents.CreateAgent(ctx, req)
	require.NoError(t, err)

	child := created.Msg.GetAgent()
	require.NotNil(t, child.ParentAgentId)
	assert.Equal(t, parent.GetId(), child.GetParentAgentId())
	assert.Equal(t, parent.GetCwd(), child.GetCwd(), "a child runs in its parent's checkout")

	t.Run("a child cannot be archived on its own", func(t *testing.T) {
		_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{
			Ids: []string{child.GetId()},
		}))
		require.Error(t, err)
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		assert.ElementsMatch(t, []string{"author", "reviewer"}, liveNames(t, instance))
	})

	t.Run("a child may only mail inside its family", func(t *testing.T) {
		stranger := createAgent(t, instance, workflowID, "unrelated")

		send := connect.NewRequest(&agentv1.SendMailRequest{
			ToAgentId: stranger.GetId(),
			Body:      "hello",
		})
		caller.Caller{AgentID: child.GetId()}.SetHeader(send.Header())

		_, err := instance.Agents.SendMail(ctx, send)
		require.Error(t, err)
		assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

		toParent := connect.NewRequest(&agentv1.SendMailRequest{
			ToAgentId: parent.GetId(),
			Body:      "the diff reads clean",
		})
		caller.Caller{AgentID: child.GetId()}.SetHeader(toParent.Header())

		_, err = instance.Agents.SendMail(ctx, toParent)
		require.NoError(t, err)
	})

	t.Run("archiving the parent takes the child", func(t *testing.T) {
		_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{
			Ids: []string{parent.GetId()},
		}))
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"unrelated"}, liveNames(t, instance))
		assert.ElementsMatch(t, []string{"author", "reviewer"}, archivedNames(t, instance))
	})

	t.Run("restoring the parent brings the child back", func(t *testing.T) {
		_, err := instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{
			Ids: []string{parent.GetId()},
		}))
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"author", "reviewer", "unrelated"}, liveNames(t, instance))
	})
}
