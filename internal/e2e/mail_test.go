//go:build integration

package e2e

import (
	"testing"
	"time"

	"komrad/internal/caller"
	"komrad/internal/harness"
	"komrad/internal/komradtest"
	"komrad/internal/mail"
	agentstore "komrad/internal/store/agent"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationAgentsMailEachOther walks one exchange between two agents, from the send through the nudge and the read to the reply.
func TestIntegrationAgentsMailEachOther(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	workflowID := createMailWorkflow(t, instance)
	api := createAgent(t, instance, workflowID, "api")
	migrations := createAgent(t, instance, workflowID, "migrations")

	instance.Settle(migrations.GetId(), harness.ActivityIdle, agentstore.StateIdle)

	var sentID string

	t.Run("an agent addresses another by name and nothing reaches it yet", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.SendMailRequest{
			To:   &agentv1.SendMailRequest_ToAgentName{ToAgentName: "migrations"},
			Body: "what is the refund column called?",
		})
		callAs(req, api.GetId())

		sent, err := instance.Agents.SendMail(ctx, req)
		require.NoError(t, err)

		sentMail := sent.Msg.GetMail()
		assert.Equal(t, migrations.GetId(), sentMail.GetToAgentId())
		assert.Equal(t, api.GetId(), sentMail.GetFromAgentId())
		assert.Equal(t, "api", sentMail.GetFromName())
		require.NotEmpty(t, sentMail.GetId())
		sentID = sentMail.GetId()

		assert.Empty(t, instance.Received(migrations.GetId()), "sending reaches nobody yet")
	})

	t.Run("delivery types one line at the idle recipient", func(t *testing.T) {
		assert.Equal(t, mail.DeliverySoon, instance.Deliver(migrations.GetId()))

		received := instance.WaitForReceived(migrations.GetId(), 1)
		require.Len(t, received, 1, "the agent received exactly one line")
		assert.Contains(t, received[0], "read_mail")
		assert.Contains(t, received[0], "1 piece(s) of unread mail")
		assert.NotContains(t, received[0], "refund column", "the mail itself stays in the panel")
	})

	t.Run("it is not nudged twice for the same stretch of sitting idle", func(t *testing.T) {
		assert.Equal(t, mail.DeliveryLater, instance.Deliver(migrations.GetId()))
		assert.Len(t, instance.Received(migrations.GetId()), 1)
	})

	t.Run("the recipient reads it once and delivery stands down", func(t *testing.T) {
		read := readMail(t, instance, migrations.GetId())
		require.Len(t, read, 1)
		assert.Equal(t, sentID, read[0].GetId())
		assert.Equal(t, "what is the refund column called?", read[0].GetBody())

		sentAt, err := time.Parse(time.RFC3339, read[0].GetCreatedAt())
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now(), sentAt, time.Minute)

		assert.Empty(t, readMail(t, instance, migrations.GetId()), "mail read once is not waiting again")
		assert.Equal(t, mail.DeliveryDone, instance.Deliver(migrations.GetId()))
	})

	t.Run("the answer comes back on the same thread", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.SendMailRequest{
			To:      &agentv1.SendMailRequest_ToAgentId{ToAgentId: api.GetId()},
			Body:    "refunded_cents",
			ReplyTo: &sentID,
		})
		callAs(req, migrations.GetId())

		_, err := instance.Agents.SendMail(ctx, req)
		require.NoError(t, err)

		read := readMail(t, instance, api.GetId())
		require.Len(t, read, 1)
		assert.Equal(t, "refunded_cents", read[0].GetBody())
		assert.Equal(t, sentID, read[0].GetReplyTo())
		assert.Equal(t, "migrations", read[0].GetFromName())
	})

	t.Run("mail dies with the agent it was addressed to", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.SendMailRequest{
			To:   &agentv1.SendMailRequest_ToAgentId{ToAgentId: migrations.GetId()},
			Body: "still there?",
		})
		callAs(req, api.GetId())
		_, err := instance.Agents.SendMail(ctx, req)
		require.NoError(t, err)

		_, err = instance.Tasks.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{
			TaskIds: []string{migrations.GetTaskId()},
			Records: true,
		}))
		require.NoError(t, err)
		instance.WorkQueue()

		gone := migrations.GetId()
		_, err = instance.Agents.ReadMail(ctx, connect.NewRequest(&agentv1.ReadMailRequest{
			Id: &gone,
		}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})
}

// TestIntegrationDeliveryWaitsOutAPermissionPrompt walks one piece of mail from an agent on a permission prompt to the same agent mid-turn.
func TestIntegrationDeliveryWaitsOutAPermissionPrompt(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	workflowID := createMailWorkflow(t, instance)
	api := createAgent(t, instance, workflowID, "api")
	migrations := createAgent(t, instance, workflowID, "migrations")

	req := connect.NewRequest(&agentv1.SendMailRequest{
		To:   &agentv1.SendMailRequest_ToAgentId{ToAgentId: migrations.GetId()},
		Body: "what is the refund column called?",
	})
	callAs(req, api.GetId())
	_, err := instance.Agents.SendMail(ctx, req)
	require.NoError(t, err)

	t.Run("an agent on a permission prompt is never told, since the line would answer it", func(t *testing.T) {
		instance.Settle(migrations.GetId(), harness.ActivityWaiting, agentstore.StateWaiting)

		assert.Equal(t, mail.DeliveryLater, instance.Deliver(migrations.GetId()))
		assert.Empty(t, instance.Received(migrations.GetId()))
	})

	t.Run("an agent mid-turn is told at once", func(t *testing.T) {
		instance.Settle(migrations.GetId(), harness.ActivityWorking, agentstore.StateThinking)

		assert.Equal(t, mail.DeliverySoon, instance.Deliver(migrations.GetId()))

		received := instance.WaitForReceived(migrations.GetId(), 1)
		require.Len(t, received, 1)
		assert.Contains(t, received[0], "read_mail")
	})
}

// TestIntegrationExpiredMailIsGivenUpOn covers delivery finishing at the deadline rather than chasing a recipient forever.
func TestIntegrationExpiredMailIsGivenUpOn(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{DeliveryDeadline: "1ns"})
	ctx := t.Context()

	workflowID := createMailWorkflow(t, instance)
	api := createAgent(t, instance, workflowID, "api")
	migrations := createAgent(t, instance, workflowID, "migrations")

	instance.Settle(migrations.GetId(), harness.ActivityIdle, agentstore.StateIdle)

	req := connect.NewRequest(&agentv1.SendMailRequest{
		To:   &agentv1.SendMailRequest_ToAgentId{ToAgentId: migrations.GetId()},
		Body: "too late",
	})
	callAs(req, api.GetId())
	_, err := instance.Agents.SendMail(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, mail.DeliveryDone, instance.Deliver(migrations.GetId()))
	assert.Empty(t, instance.Received(migrations.GetId()))
	assert.Empty(t, readMail(t, instance, migrations.GetId()), "expired mail is not readable either")
}

func createMailWorkflow(t *testing.T, instance *komradtest.Instance) string {
	t.Helper()

	workflow, err := instance.Workflows.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "checkout",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	return workflow.Msg.GetWorkflow().GetId()
}

func callAs[T any](req *connect.Request[T], agentID string) {
	caller.Caller{AgentID: agentID}.SetHeader(req.Header())
}

func createAgent(t *testing.T, instance *komradtest.Instance, workflowID, name string) *agentv1.Agent {
	t.Helper()

	created, err := instance.Agents.CreateAgent(t.Context(), connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "wait for mail",
		Title:      name,
		WorkflowId: workflowID,
	}))
	require.NoError(t, err)

	return created.Msg.GetAgent()
}

func readMail(t *testing.T, instance *komradtest.Instance, agentID string) []*agentv1.Mail {
	t.Helper()

	req := connect.NewRequest(&agentv1.ReadMailRequest{})
	callAs(req, agentID)

	read, err := instance.Agents.ReadMail(t.Context(), req)
	require.NoError(t, err)

	return read.Msg.GetMail()
}
