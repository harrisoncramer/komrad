package e2e

import (
	"testing"
	"time"

	"weave/internal/caller"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentsMessageEachOther(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "checkout",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	api := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "api")
	migrations := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "migrations")

	var sentID string

	t.Run("an agent addresses another by id", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.SendMessageRequest{
			ToAgentId: migrations.GetId(),
			Body:      "what is the refund column called?",
		})
		caller.Caller{AgentID: api.GetId()}.SetHeader(req.Header())

		sent, err := instance.Agents.SendMessage(ctx, req)
		require.NoError(t, err)

		message := sent.Msg.GetMessage()
		assert.Equal(t, migrations.GetId(), message.GetToAgentId())
		assert.Equal(t, api.GetId(), message.GetFromAgentId())
		assert.Equal(t, "api", message.GetFromName())
		require.NotEmpty(t, message.GetId())
		sentID = message.GetId()
	})

	t.Run("the recipient reads it once", func(t *testing.T) {
		read := readMail(t, instance, migrations.GetId())
		require.Len(t, read, 1)
		assert.Equal(t, "what is the refund column called?", read[0].GetBody())
		assert.Equal(t, sentID, read[0].GetId())

		sentAt, err := time.Parse(time.RFC3339, read[0].GetCreatedAt())
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now(), sentAt, time.Minute)

		assert.Empty(t, readMail(t, instance, migrations.GetId()), "a message read once is not waiting again")
	})

	t.Run("the answer comes back on the same thread", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.SendMessageRequest{
			ToAgentId: api.GetId(),
			Body:      "refunded_cents",
			ReplyTo:   &sentID,
		})
		caller.Caller{AgentID: migrations.GetId()}.SetHeader(req.Header())

		_, err := instance.Agents.SendMessage(ctx, req)
		require.NoError(t, err)

		read := readMail(t, instance, api.GetId())
		require.Len(t, read, 1)
		assert.Equal(t, "refunded_cents", read[0].GetBody())
		assert.Equal(t, sentID, read[0].GetReplyTo())
		assert.Equal(t, "migrations", read[0].GetFromName())
	})

	t.Run("mail dies with the agent it was addressed to", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.SendMessageRequest{ToAgentId: migrations.GetId(), Body: "still there?"})
		caller.Caller{AgentID: api.GetId()}.SetHeader(req.Header())
		_, err := instance.Agents.SendMessage(ctx, req)
		require.NoError(t, err)

		_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{
			Ids: []string{migrations.GetId()},
		}))
		require.NoError(t, err)

		gone := migrations.GetId()
		_, err = instance.Agents.ReadMessages(ctx, connect.NewRequest(&agentv1.ReadMessagesRequest{
			Id: &gone,
		}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})
}

func createAgent(t *testing.T, instance *weavetest.Instance, workflowID, name string) *agentv1.Agent {
	t.Helper()

	created, err := instance.Agents.CreateAgent(t.Context(), connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "wait for mail",
		Title:      name,
		WorkflowId: workflowID,
	}))
	require.NoError(t, err)

	return created.Msg.GetAgent()
}

func readMail(t *testing.T, instance *weavetest.Instance, agentID string) []*agentv1.Message {
	t.Helper()

	req := connect.NewRequest(&agentv1.ReadMessagesRequest{})
	caller.Caller{AgentID: agentID}.SetHeader(req.Header())

	read, err := instance.Agents.ReadMessages(t.Context(), req)
	require.NoError(t, err)

	return read.Msg.GetMessages()
}
