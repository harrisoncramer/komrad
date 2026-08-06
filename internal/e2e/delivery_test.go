package e2e

import (
	"testing"

	"weave/internal/agents"
	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMailReachesAnAgentThatHadAlreadyStopped is the case the hooks alone cannot cover. The
// recipient is sitting idle when the mail is sent, so no turn is ending and nothing runs
// on its behalf. Delivery has to notice, type at it, and stand down once the mail is read.
func TestMailReachesAnAgentThatHadAlreadyStopped(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "checkout",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	api := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "api")
	migrations := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "migrations")

	instance.Settle(migrations.GetId(), harness.ActivityIdle, agentstore.StateIdle)

	var sentID string

	t.Run("mail is sent to an agent that has already stopped", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.SendMailRequest{
			ToAgentId: migrations.GetId(),
			Body:      "what is the refund column called?",
		})
		callAs(req, api.GetId())

		sent, err := instance.Agents.SendMail(ctx, req)
		require.NoError(t, err)
		sentID = sent.Msg.GetMail().GetId()

		assert.Empty(t, instance.Typed(migrations.GetId()), "sending types nothing at anybody")
	})

	t.Run("delivery types one line at it", func(t *testing.T) {
		assert.Equal(t, agents.DeliverySoon, instance.Deliver(migrations.GetId()))

		typed := instance.WaitForTyped(migrations.GetId(), 1)
		require.Len(t, typed, 1, "the agent received exactly one line")
		assert.Contains(t, typed[0], "read_mail")
		assert.Contains(t, typed[0], "1 unread")
		assert.NotContains(t, typed[0], "refund column", "the mail itself stays in the panel")
	})

	t.Run("it is not typed at twice for the same stretch of sitting idle", func(t *testing.T) {
		assert.Equal(t, agents.DeliveryLater, instance.Deliver(migrations.GetId()))
		assert.Len(t, instance.Typed(migrations.GetId()), 1)
	})

	t.Run("the agent reads its mail and delivery stands down", func(t *testing.T) {
		read := instance.EndTurn(migrations.GetId())
		require.Len(t, read, 1)
		assert.Equal(t, sentID, read[0].GetId())
		assert.Equal(t, "api", read[0].GetFromName())

		assert.Equal(t, agents.DeliveryDone, instance.Deliver(migrations.GetId()))
	})

	t.Run("what it was told is searchable afterwards", func(t *testing.T) {
		instance.SyncTranscripts()

		scope := migrations.GetId()
		hits, err := instance.Agents.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
			Query:   "refund column",
			AgentId: &scope,
		}))
		require.NoError(t, err)
		require.NotEmpty(t, hits.Msg.GetHits())
	})
}

// TestDeliveryLeavesABusyAgentAlone covers the other half of the ladder. An agent mid-turn
// is about to run its own stop hook, so typing at it would cost a turn for nothing.
func TestDeliveryLeavesABusyAgentAlone(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "checkout",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	api := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "api")
	migrations := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "migrations")

	instance.Settle(migrations.GetId(), harness.ActivityWorking, agentstore.StateThinking)

	req := connect.NewRequest(&agentv1.SendMailRequest{
		ToAgentId: migrations.GetId(),
		Body:      "refunded_cents",
	})
	callAs(req, api.GetId())
	_, err = instance.Agents.SendMail(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, agents.DeliverySoon, instance.Deliver(migrations.GetId()))
	assert.Empty(t, instance.Typed(migrations.GetId()), "an agent mid-turn is never typed at")

	require.Len(t, instance.EndTurn(migrations.GetId()), 1)
	assert.Equal(t, agents.DeliveryDone, instance.Deliver(migrations.GetId()))
}

// TestDeliveryNeverTypesAtAnAgentOnAPrompt covers the state that must never be typed at,
// since a line sent to a permission prompt answers the prompt.
func TestDeliveryNeverTypesAtAnAgentOnAPrompt(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "checkout",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	api := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "api")
	migrations := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "migrations")

	instance.Settle(migrations.GetId(), harness.ActivityWaiting, agentstore.StateWaiting)

	req := connect.NewRequest(&agentv1.SendMailRequest{
		ToAgentId: migrations.GetId(),
		Body:      "are you stuck?",
	})
	callAs(req, api.GetId())
	_, err = instance.Agents.SendMail(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, agents.DeliveryLater, instance.Deliver(migrations.GetId()))
	assert.Empty(t, instance.Typed(migrations.GetId()))
}

// TestExpiredMailIsGivenUpOn covers the deadline. Nothing is typed, nothing is readable, and
// delivery finishes rather than chasing a recipient forever.
func TestExpiredMailIsGivenUpOn(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{DeliveryDeadline: "1ns"})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "checkout",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	api := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "api")
	migrations := createAgent(t, instance, workflow.Msg.GetWorkflow().GetId(), "migrations")

	instance.Settle(migrations.GetId(), harness.ActivityIdle, agentstore.StateIdle)

	req := connect.NewRequest(&agentv1.SendMailRequest{
		ToAgentId: migrations.GetId(),
		Body:      "too late",
	})
	callAs(req, api.GetId())
	_, err = instance.Agents.SendMail(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, agents.DeliveryDone, instance.Deliver(migrations.GetId()))
	assert.Empty(t, instance.Typed(migrations.GetId()))
	assert.Empty(t, instance.EndTurn(migrations.GetId()), "expired mail is not readable either")
}
