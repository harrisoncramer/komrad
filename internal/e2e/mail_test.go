//go:build integration

package e2e

import (
	"testing"
	"time"

	"weave/internal/caller"
	"weave/internal/harness"
	"weave/internal/mail"
	agentstore "weave/internal/store/agent"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationAgentsMailEachOther walks one exchange between two agents, from the send through the nudge and the read to the reply.
func TestIntegrationAgentsMailEachOther(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflowID := createMailWorkflow(t, instance)
	api := createAgent(t, instance, workflowID, "api")
	migrations := createAgent(t, instance, workflowID, "migrations")

	instance.Settle(migrations.GetId(), harness.ActivityIdle, agentstore.StateIdle)

	var sentID string

	t.Run("an agent addresses another by name and nothing is typed yet", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.SendMailRequest{
			To:   "migrations",
			Body: "what is the refund column called?",
		})
		callAs(req, api.GetId())

		sent, err := instance.Agents.SendMail(ctx, req)
		require.NoError(t, err)

		mail := sent.Msg.GetMail()
		assert.Equal(t, migrations.GetId(), mail.GetToAgentId())
		assert.Equal(t, api.GetId(), mail.GetFromAgentId())
		assert.Equal(t, "api", mail.GetFromName())
		require.NotEmpty(t, mail.GetId())
		sentID = mail.GetId()

		assert.Empty(t, instance.Typed(migrations.GetId()), "sending types nothing at anybody")
	})

	t.Run("delivery types one line at the idle recipient", func(t *testing.T) {
		assert.Equal(t, mail.DeliverySoon, instance.Deliver(migrations.GetId()))

		typed := instance.WaitForTyped(migrations.GetId(), 1)
		require.Len(t, typed, 1, "the agent received exactly one line")
		assert.Contains(t, typed[0], "read_mail")
		assert.Contains(t, typed[0], "1 piece(s) of unread mail")
		assert.NotContains(t, typed[0], "refund column", "the mail itself stays in the panel")
	})

	t.Run("it is not typed at twice for the same stretch of sitting idle", func(t *testing.T) {
		assert.Equal(t, mail.DeliveryLater, instance.Deliver(migrations.GetId()))
		assert.Len(t, instance.Typed(migrations.GetId()), 1)
	})

	t.Run("the recipient reads it once and delivery stands down", func(t *testing.T) {
		read := instance.EndTurn(migrations.GetId())
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
			To:      api.GetId(),
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

	t.Run("what it was told is searchable afterwards", func(t *testing.T) {
		instance.SyncTranscripts()

		scope := migrations.GetId()
		hits, err := instance.Agents.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
			Query:   "refund column",
			AgentId: &scope,
		}))
		require.NoError(t, err)
		require.NotEmpty(t, hits.Msg.GetMatches())
	})

	t.Run("mail dies with the agent it was addressed to", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.SendMailRequest{
			To:   migrations.GetId(),
			Body: "still there?",
		})
		callAs(req, api.GetId())
		_, err := instance.Agents.SendMail(ctx, req)
		require.NoError(t, err)

		_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{
			Ids: []string{migrations.GetId()},
		}))
		require.NoError(t, err)

		gone := migrations.GetId()
		_, err = instance.Agents.ReadMail(ctx, connect.NewRequest(&agentv1.ReadMailRequest{
			Id: &gone,
		}))
		assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})
}

// TestIntegrationDeliveryWaitsUntilTheRecipientCanTakeTheLine walks one piece of mail past every state an agent must not be typed at in.
func TestIntegrationDeliveryWaitsUntilTheRecipientCanTakeTheLine(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflowID := createMailWorkflow(t, instance)
	api := createAgent(t, instance, workflowID, "api")
	migrations := createAgent(t, instance, workflowID, "migrations")

	req := connect.NewRequest(&agentv1.SendMailRequest{
		To:   migrations.GetId(),
		Body: "what is the refund column called?",
	})
	callAs(req, api.GetId())
	_, err := instance.Agents.SendMail(ctx, req)
	require.NoError(t, err)

	t.Run("an agent mid-turn is left to its own stop hook", func(t *testing.T) {
		instance.Settle(migrations.GetId(), harness.ActivityWorking, agentstore.StateThinking)

		assert.Equal(t, mail.DeliverySoon, instance.Deliver(migrations.GetId()))
		assert.Empty(t, instance.Typed(migrations.GetId()))
	})

	t.Run("an agent on a permission prompt is never typed at, since the line would answer it", func(t *testing.T) {
		instance.Settle(migrations.GetId(), harness.ActivityWaiting, agentstore.StateWaiting)

		assert.Equal(t, mail.DeliveryLater, instance.Deliver(migrations.GetId()))
		assert.Empty(t, instance.Typed(migrations.GetId()))
	})

	t.Run("a pane yet to draw its prompt is never typed at, however idle the row reads", func(t *testing.T) {
		instance.Settle(migrations.GetId(), harness.ActivityIdle, agentstore.StateIdle)
		require.NoError(t, instance.Harness.Show(migrations.GetId(), "fake:coming-up"))
		instance.WaitForActivity(migrations.GetId(), harness.ActivityUnknown)

		assert.Equal(t, mail.DeliveryLater, instance.Deliver(migrations.GetId()))
		assert.Empty(t, instance.Typed(migrations.GetId()))
	})

	t.Run("the agent is told once its prompt is there to take the line", func(t *testing.T) {
		instance.Settle(migrations.GetId(), harness.ActivityIdle, agentstore.StateIdle)

		assert.Equal(t, mail.DeliverySoon, instance.Deliver(migrations.GetId()))

		typed := instance.WaitForTyped(migrations.GetId(), 1)
		require.Len(t, typed, 1)
		assert.Contains(t, typed[0], "read_mail")
	})
}

// TestIntegrationExpiredMailIsGivenUpOn covers delivery finishing at the deadline rather than chasing a recipient forever.
func TestIntegrationExpiredMailIsGivenUpOn(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{DeliveryDeadline: "1ns"})
	ctx := t.Context()

	workflowID := createMailWorkflow(t, instance)
	api := createAgent(t, instance, workflowID, "api")
	migrations := createAgent(t, instance, workflowID, "migrations")

	instance.Settle(migrations.GetId(), harness.ActivityIdle, agentstore.StateIdle)

	req := connect.NewRequest(&agentv1.SendMailRequest{
		To:   migrations.GetId(),
		Body: "too late",
	})
	callAs(req, api.GetId())
	_, err := instance.Agents.SendMail(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, mail.DeliveryDone, instance.Deliver(migrations.GetId()))
	assert.Empty(t, instance.Typed(migrations.GetId()))
	assert.Empty(t, instance.EndTurn(migrations.GetId()), "expired mail is not readable either")
}

func createMailWorkflow(t *testing.T, instance *weavetest.Instance) string {
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

func readMail(t *testing.T, instance *weavetest.Instance, agentID string) []*agentv1.Mail {
	t.Helper()

	req := connect.NewRequest(&agentv1.ReadMailRequest{})
	callAs(req, agentID)

	read, err := instance.Agents.ReadMail(t.Context(), req)
	require.NoError(t, err)

	return read.Msg.GetMail()
}
