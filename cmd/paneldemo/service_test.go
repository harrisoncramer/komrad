package main

import (
	"net/http/httptest"
	"testing"

	"weave/internal/client"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// served starts the fake service and hands back the clients the panel would hold.
func served(t *testing.T) (agentv1connect.AgentServiceClient, workflowv1connect.WorkflowServiceClient, schedulev1connect.ScheduleServiceClient) {
	t.Helper()

	server := httptest.NewServer(serviceFor(newCast()))
	t.Cleanup(server.Close)

	return agentv1connect.NewAgentServiceClient(server.Client(), server.URL),
		workflowv1connect.NewWorkflowServiceClient(server.Client(), server.URL),
		schedulev1connect.NewScheduleServiceClient(server.Client(), server.URL)
}

// TestThePanelsOwnListCallsAllAnswer covers the three calls a refresh makes, which are what has
// to work before any screen appears.
func TestThePanelsOwnListCallsAllAnswer(t *testing.T) {
	agents, workflows, schedules := served(t)
	ctx := t.Context()

	listedAgents, err := client.ListAllAgents(ctx, agents)
	require.NoError(t, err)
	assert.NotEmpty(t, listedAgents)

	listedWorkflows, err := client.ListAllWorkflows(ctx, workflows)
	require.NoError(t, err)
	assert.NotEmpty(t, listedWorkflows)

	listedSchedules, err := client.ListAllSchedules(ctx, schedules)
	require.NoError(t, err)
	assert.NotEmpty(t, listedSchedules)
}

// TestTheArchiveIsItsOwnListing covers the archived agents never appearing among the live ones.
func TestTheArchiveIsItsOwnListing(t *testing.T) {
	agents, _, _ := served(t)
	ctx := t.Context()

	live, err := client.ListAllAgents(ctx, agents)
	require.NoError(t, err)

	archived, err := client.ListAllArchivedAgents(ctx, agents)
	require.NoError(t, err)
	require.NotEmpty(t, archived)

	for _, agent := range live {
		assert.Empty(t, agent.GetArchivedAt(), "%s is live and carries an archived time", agent.GetName())
	}

	for _, agent := range archived {
		assert.NotEmpty(t, agent.GetArchivedAt(), "%s is archived and carries none", agent.GetName())
	}
}

// TestMailIsAnsweredForTheAgentAndDirectionAsked covers a mailbox drawing each message once, on
// the side it belongs to.
func TestMailIsAnsweredForTheAgentAndDirectionAsked(t *testing.T) {
	agents, _, _ := served(t)
	ctx := t.Context()

	for _, direction := range []agentv1.MailDirection{
		agentv1.MailDirection_MAIL_DIRECTION_RECEIVED,
		agentv1.MailDirection_MAIL_DIRECTION_SENT,
	} {
		resp, err := agents.ListMail(ctx, connect.NewRequest(&agentv1.ListMailRequest{
			Id:        ptr("agent-mockup"),
			Direction: direction,
		}))
		require.NoError(t, err)
		require.NotEmpty(t, resp.Msg.GetMail(), "no mail for direction %v", direction)

		for _, mail := range resp.Msg.GetMail() {
			if direction == agentv1.MailDirection_MAIL_DIRECTION_SENT {
				assert.Equal(t, "agent-mockup", mail.GetFromAgentId())

				continue
			}

			assert.Equal(t, "agent-mockup", mail.GetToAgentId())
		}
	}
}

// TestAnUnwrittenCallIsAnErrorRatherThanACrash covers the whole point of embedding the generated
// handler, which is that a screen reaching a call nobody wrote draws a message.
func TestAnUnwrittenCallIsAnErrorRatherThanACrash(t *testing.T) {
	agents, _, _ := served(t)

	_, err := agents.CreateAgent(t.Context(), connect.NewRequest(&agentv1.CreateAgentRequest{}))

	require.Error(t, err)
	assert.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err))
}
