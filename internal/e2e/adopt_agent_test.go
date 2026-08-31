package e2e

import (
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	startTimeout = 10 * time.Second
	startPoll    = 25 * time.Millisecond
)

// TestAdoptAgentPicksUpSomebodysConversation walks an adoption from a conversation on disk that
// nothing in the panel started through to an agent with a pane, a briefing, and a history the
// panel can search, which is the whole point of adopting one.
func TestAdoptAgentPicksUpSomebodysConversation(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	theirs := t.TempDir()
	source := uuid.NewString()
	require.NoError(t, instance.Harness.Seed(source, theirs, "someones/work",
		harness.Turn{Role: "user", Content: "the checkout job double charges on retry"},
		harness.Turn{Role: "assistant", Content: "the idempotency key is minted inside the retry loop"},
	))

	adopted, err := instance.Agents.AdoptAgent(ctx, connect.NewRequest(&agentv1.AdoptAgentRequest{
		SessionId:  source,
		Prompt:     "finish what they started",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	agent := adopted.Msg.GetAgent()
	require.NotEmpty(t, agent.GetPaneId())
	assert.Equal(t, agent.GetPaneId(), instance.GetOwnedPane(agent.GetId()), "the adopted agent holds a pane of its own")
	assert.Equal(t, theirs, agent.GetCwd(), "and runs where the conversation was held")
	assert.NotEqual(t, source, agent.GetSessionId(), "the panel forks rather than writing into the person's own conversation")

	// The CLI is typed into a pane, so the fork it carries over lands a moment after the call
	// that started it returns.
	require.Eventually(t, func() bool {
		return instance.Harness.GetLogPath(agent.GetSessionId()) != ""
	}, startTimeout, startPoll, "the adopted agent never opened a conversation of its own")

	t.Run("the history comes with it", func(t *testing.T) {
		instance.SyncTranscripts()

		hits, err := instance.Agents.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
			Query: "idempotency key",
		}))
		require.NoError(t, err)

		require.Len(t, hits.Msg.GetHits(), 1, "a turn from before the adoption is searchable")
		assert.Equal(t, agent.GetId(), hits.Msg.GetHits()[0].GetAgentId())
	})

	t.Run("the person's own conversation is untouched", func(t *testing.T) {
		read, err := instance.Harness.ReadLog(instance.Harness.GetLogPath(source), 0)
		require.NoError(t, err)

		require.Len(t, read.Turns, 2, "nothing was appended to the log the person keeps")
	})

	t.Run("the agent knows where it is", func(t *testing.T) {
		require.Eventually(t, func() bool {
			return instance.Harness.Context(agent.GetId()) != ""
		}, startTimeout, startPoll, "the adopted agent was never briefed")

		assert.Contains(t, instance.Harness.Context(agent.GetId()), "weave",
			"an adopted agent is briefed, since a resume carries no briefing of its own")
	})

	t.Run("adopting it again hands back the same agent", func(t *testing.T) {
		again, err := instance.Agents.AdoptAgent(ctx, connect.NewRequest(&agentv1.AdoptAgentRequest{
			SessionId:  source,
			WorkflowId: workflow.Msg.GetWorkflow().GetId(),
		}))
		require.NoError(t, err)

		assert.Equal(t, agent.GetId(), again.Msg.GetAgent().GetId())
	})
}
