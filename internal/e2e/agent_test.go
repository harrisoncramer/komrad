//go:build integration

package e2e

import (
	"testing"

	"komrade/internal/harness"
	"komrade/internal/komradetest"
	agentstore "komrade/internal/store/agent"
	"komrade/lib/xslice"
	agentv1 "komrade/proto/gen/agent/v1"
	workflowv1 "komrade/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationAgentLifecycle walks an agent from created to destroyed, asserting the pane alongside
// the row since the two parting company leaves an agent nobody can reach.
func TestIntegrationAgentLifecycle(t *testing.T) {
	instance := komradetest.Start(t, komradetest.Params{})
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
		assert.Equal(t, "login-race", got.Msg.GetAgent().GetAgent().GetName())
	})

	t.Run("a person holds the agent at rest and lets it go", func(t *testing.T) {
		held, err := instance.Agents.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{
			Id: agent.GetId(),
		}))
		require.NoError(t, err)
		assert.NotEmpty(t, held.Msg.GetAgent().GetHeldAt())

		_, err = instance.Agents.ResumeAgent(ctx, connect.NewRequest(&agentv1.ResumeAgentRequest{
			Id: agent.GetId(),
		}))
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "a held agent is left alone")

		released, err := instance.Agents.ReleaseAgent(ctx, connect.NewRequest(&agentv1.ReleaseAgentRequest{
			Id: agent.GetId(),
		}))
		require.NoError(t, err)
		assert.Empty(t, released.Msg.GetAgent().GetHeldAt())
		assert.Empty(t, instance.Typed(agent.GetId()), "an agent that had finished is told nothing")
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

// TestIntegrationAgentsSpawnAgents walks an agent spawning another on its own and a child attached
// to it.
func TestIntegrationAgentsSpawnAgents(t *testing.T) {
	instance := komradetest.Start(t, komradetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "komrade",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)
	workflowID := workflow.Msg.GetWorkflow().GetId()

	parent := createAgent(t, instance, workflowID, "author")
	var child *agentv1.Agent

	t.Run("a person creating from the panel records no spawner", func(t *testing.T) {
		assert.Nil(t, parent.SpawnedByAgentId)
	})

	t.Run("an agent spawning another on its own records itself as the spawner", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:     "fix the docs",
			Title:      "unrelated",
			WorkflowId: workflowID,
		})
		callAs(req, parent.GetId())

		created, err := instance.Agents.CreateAgent(ctx, req)
		require.NoError(t, err)

		spawned := created.Msg.GetAgent()
		assert.Equal(t, parent.GetId(), spawned.GetSpawnedByAgentId())
		assert.Nil(t, spawned.ParentAgentId, "an agent spawned on its own is nobody's child")
	})

	t.Run("a child runs in its parent's checkout", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:        "read the change",
			Title:         "reviewer",
			WorkflowId:    workflowID,
			ParentAgentId: &parent.Id,
		})
		callAs(req, parent.GetId())

		created, err := instance.Agents.CreateAgent(ctx, req)
		require.NoError(t, err)

		child = created.Msg.GetAgent()
		assert.Equal(t, parent.GetId(), child.GetParentAgentId())
		assert.Equal(t, parent.GetCwd(), child.GetCwd())
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

	t.Run("a child is archived on its own", func(t *testing.T) {
		_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{
			Ids: []string{child.GetId()},
		}))
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"author", "unrelated"}, liveNames(t, instance))
		assert.ElementsMatch(t, []string{"reviewer"}, archivedNames(t, instance))
	})

	t.Run("a child cannot come back before its parent", func(t *testing.T) {
		_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{
			Ids: []string{parent.GetId()},
		}))
		require.NoError(t, err)

		_, err = instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{
			Ids: []string{child.GetId()},
		}))
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		assert.ElementsMatch(t, []string{"author", "reviewer"}, archivedNames(t, instance))

		_, err = instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{
			Ids: []string{parent.GetId(), child.GetId()},
		}))
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"author", "reviewer", "unrelated"}, liveNames(t, instance))
	})
}

func liveNames(t *testing.T, instance *komradetest.Instance) []string {
	t.Helper()

	return names(t, instance, false)
}

func archivedNames(t *testing.T, instance *komradetest.Instance) []string {
	t.Helper()

	return names(t, instance, true)
}

func names(t *testing.T, instance *komradetest.Instance, archived bool) []string {
	t.Helper()

	listed, err := instance.Agents.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{
		Archived: archived,
	}))
	require.NoError(t, err)

	found := xslice.Map(listed.Msg.GetAgents(), func(row *agentv1.AgentRow) string {
		return row.GetAgent().GetName()
	})

	return found
}
