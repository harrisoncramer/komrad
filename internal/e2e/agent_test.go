//go:build integration

package e2e

import (
	"testing"

	"weave/internal/caller"
	"weave/internal/harness"
	agentstore "weave/internal/store/agent"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentLifecycle walks an agent from created to destroyed, asserting the pane alongside
// the row since the two parting company leaves an agent nobody can reach.
func TestAgentLifecycle(t *testing.T) {
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

	agent := created.Msg.GetAgent()
	assert.Equal(t, "login", agent.GetName())
	assert.Equal(t, workflow.Msg.GetWorkflow().GetId(), agent.GetWorkflowId())
	require.NotEmpty(t, agent.GetPaneId())

	assert.Equal(t, agent.GetPaneId(), instance.GetOwnedPane(agent.GetId()))

	t.Run("the reconciler follows what the pane shows", func(t *testing.T) {
		instance.Settle(agent.GetId(), harness.ActivityWorking, agentstore.StateThinking)
		instance.Settle(agent.GetId(), harness.ActivityWaiting, agentstore.StateWaiting)
		instance.Settle(agent.GetId(), harness.ActivityIdle, agentstore.StateIdle)
	})

	t.Run("what was said is searchable", func(t *testing.T) {
		require.NoError(t, instance.Harness.Say(agent.GetSessionId(), "assistant", "the login test races the token refresh"))
		instance.SyncTranscripts()

		hits, err := instance.Agents.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
			Query: "token refresh",
		}))
		require.NoError(t, err)

		require.Len(t, hits.Msg.GetMatches(), 1)
		assert.Equal(t, agent.GetId(), hits.Msg.GetMatches()[0].GetAgentId())
	})

	t.Run("a rename keeps the same agent", func(t *testing.T) {
		_, err := instance.Agents.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
			Id:   agent.GetId(),
			Name: "login-race",
		}))
		require.NoError(t, err)

		got, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agent.GetId()}))
		require.NoError(t, err)
		assert.Equal(t, "login-race", got.Msg.GetAgent().GetName())
	})

	t.Run("archiving takes the pane with it", func(t *testing.T) {
		_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{
			Ids: []string{agent.GetId()},
		}))
		require.NoError(t, err)

		assert.Empty(t, liveNames(t, instance), "an archived agent is off the live listing")
		assert.Equal(t, []string{"login-race"}, archivedNames(t, instance))
		assert.Empty(t, instance.GetOwnedPane(agent.GetId()), "the pane goes with the agent")
	})

	t.Run("restoring puts it back with nothing running", func(t *testing.T) {
		_, err := instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{
			Ids: []string{agent.GetId()},
		}))
		require.NoError(t, err)

		assert.Equal(t, []string{"login-race"}, liveNames(t, instance))
		assert.Empty(t, archivedNames(t, instance))
	})

	t.Run("resuming reopens the same conversation", func(t *testing.T) {
		resumed, err := instance.Agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{
			Id: agent.GetId(),
		}))
		require.NoError(t, err)

		back := resumed.Msg.GetAgent()
		assert.Equal(t, agent.GetSessionId(), back.GetSessionId(), "a resume reopens the conversation rather than starting one")
		require.NotEmpty(t, back.GetPaneId())
		assert.Equal(t, back.GetPaneId(), instance.GetOwnedPane(agent.GetId()))

		instance.Tmux.WaitForFrame(back.GetPaneId(), "fake:")
		instance.SyncTranscripts()

		transcript, err := instance.Agents.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{
			AgentId: agent.GetId(),
		}))
		require.NoError(t, err)
		assert.Contains(t, transcript.Msg.GetContent(), "resumed")
	})

	t.Run("deleting leaves nothing behind", func(t *testing.T) {
		_, err := instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{
			Ids: []string{agent.GetId()},
		}))
		require.NoError(t, err)

		assert.Empty(t, liveNames(t, instance))
		assert.Empty(t, archivedNames(t, instance))
		assert.Empty(t, instance.GetOwnedPane(agent.GetId()), "a destroyed agent leaves no pane running")
	})
}

// TestCreateAgentRecordsSpawnedBy covers the parent recorded when one agent creates another,
// which arrives in the caller header and is absent on every create the panel makes.
func TestCreateAgentRecordsSpawnedBy(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)
	workflowID := workflow.Msg.GetWorkflow().GetId()

	t.Run("a person creating from the panel records no parent", func(t *testing.T) {
		agent := createAgent(t, instance, workflowID, "solo")
		assert.Nil(t, agent.SpawnedByAgentId)
	})

	t.Run("an agent creating another records its own id", func(t *testing.T) {
		parent := createAgent(t, instance, workflowID, "implementer")

		req := connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:     "review the diff",
			Title:      "reviewer",
			WorkflowId: workflowID,
		})
		caller.Caller{AgentID: parent.GetId()}.SetHeader(req.Header())

		created, err := instance.Agents.CreateAgent(ctx, req)
		require.NoError(t, err)

		child := created.Msg.GetAgent()
		require.NotNil(t, child.SpawnedByAgentId)
		assert.Equal(t, parent.GetId(), child.GetSpawnedByAgentId())
	})
}

func liveNames(t *testing.T, instance *weavetest.Instance) []string {
	t.Helper()

	return names(t, instance, false)
}

func archivedNames(t *testing.T, instance *weavetest.Instance) []string {
	t.Helper()

	return names(t, instance, true)
}

func names(t *testing.T, instance *weavetest.Instance, archived bool) []string {
	t.Helper()

	listed, err := instance.Agents.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{
		Archived: archived,
	}))
	require.NoError(t, err)

	found := make([]string, 0, len(listed.Msg.GetAgents()))
	for _, agent := range listed.Msg.GetAgents() {
		found = append(found, agent.GetName())
	}

	return found
}
