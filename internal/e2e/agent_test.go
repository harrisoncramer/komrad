//go:build integration

package e2e

import (
	"testing"

	"komrad/internal/caller"
	"komrad/internal/harness"
	"komrad/internal/komradtest"
	agentstore "komrad/internal/store/agent"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationAgentLifecycle walks an agent from created to destroyed, asserting the pane alongside
// the row since the two parting company leaves an agent nobody can reach.
func TestIntegrationAgentLifecycle(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{})
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

	t.Run("a turn an API error ended stands until the next one ends", func(t *testing.T) {
		report := func(sent *agentv1.RecordReportRequest) {
			req := connect.NewRequest(sent)
			caller.Caller{AgentID: agent.GetId(), SessionID: agent.GetSessionId()}.SetHeader(req.Header())
			_, err := instance.Agents.RecordReport(ctx, req)
			require.NoError(t, err)
		}

		instance.Settle(agent.GetId(), harness.ActivityWorking, agentstore.StateThinking)
		report(&agentv1.RecordReportRequest{
			Event:     agentv1.ReportEvent_REPORT_EVENT_TURN_FAILED,
			Hook:      "StopFailure",
			TurnError: "Overloaded",
		})
		instance.Settle(agent.GetId(), harness.ActivityIdle, agentstore.StateErrored)

		failed, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agent.GetId()}))
		require.NoError(t, err)
		assert.Equal(t, "Overloaded", failed.Msg.GetAgent().GetTurnError())

		instance.Settle(agent.GetId(), harness.ActivityWorking, agentstore.StateThinking)
		report(&agentv1.RecordReportRequest{Event: agentv1.ReportEvent_REPORT_EVENT_TURN_ENDED, Hook: "Stop"})
		instance.Settle(agent.GetId(), harness.ActivityIdle, agentstore.StateIdle)

		recovered, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agent.GetId()}))
		require.NoError(t, err)
		assert.Empty(t, recovered.Msg.GetAgent().GetTurnError())
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

	t.Run("archiving its task takes the pane with it", func(t *testing.T) {
		_, err := instance.Tasks.ArchiveTask(ctx, connect.NewRequest(&taskv1.ArchiveTaskRequest{
			Task: agent.GetTaskId(),
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

	t.Run("restarting starts a new conversation and keeps the earlier one", func(t *testing.T) {
		restarted, err := instance.Agents.RestartAgent(ctx, connect.NewRequest(&agentv1.RestartAgentRequest{
			Id:   agent.GetId(),
			Note: "pick up the cookie fix",
		}))
		require.NoError(t, err)

		fresh := restarted.Msg.GetAgent()
		assert.NotEqual(t, agent.GetSessionId(), fresh.GetSessionId())
		require.NotEmpty(t, fresh.GetPaneId())
		assert.Equal(t, fresh.GetPaneId(), instance.GetOwnedPane(agent.GetId()))

		instance.Tmux.WaitForFrame(fresh.GetPaneId(), "fake:")
		instance.SyncTranscripts()

		transcript, err := instance.Agents.GetTranscript(ctx, connect.NewRequest(&agentv1.GetTranscriptRequest{
			AgentId: agent.GetId(),
		}))
		require.NoError(t, err)
		assert.Contains(t, transcript.Msg.GetContent(), "token refresh", "the earlier conversation stays")
		assert.Contains(t, transcript.Msg.GetContent(), "on a new conversation", "and the new one follows it")
	})

	t.Run("deleting its task leaves nothing behind", func(t *testing.T) {
		_, err := instance.Tasks.DeleteTasks(ctx, connect.NewRequest(&taskv1.DeleteTasksRequest{
			Tasks:   []string{agent.GetTaskId()},
			Records: true,
		}))
		require.NoError(t, err)
		instance.WorkQueue()

		assert.Empty(t, liveNames(t, instance))
		assert.Empty(t, archivedNames(t, instance))
		assert.Empty(t, instance.GetOwnedPane(agent.GetId()), "a destroyed agent leaves no pane running")
	})
}

// TestIntegrationAgentsSpawnAgents walks an agent spawning another on its own and a reviewer put
// on its task.
func TestIntegrationAgentsSpawnAgents(t *testing.T) {
	instance := komradtest.Start(t, komradtest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "komrad",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)
	workflowID := workflow.Msg.GetWorkflow().GetId()

	writer := createAgent(t, instance, workflowID, "author")
	var reviewer *agentv1.Agent

	t.Run("a person creating from the panel records no spawner", func(t *testing.T) {
		assert.Nil(t, writer.SpawnedByAgentId)
	})

	t.Run("an agent spawning another on its own records itself as the spawner", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:     "fix the docs",
			Title:      "unrelated",
			WorkflowId: workflowID,
		})
		callAs(req, writer.GetId())

		created, err := instance.Agents.CreateAgent(ctx, req)
		require.NoError(t, err)

		spawned := created.Msg.GetAgent()
		assert.Equal(t, writer.GetId(), spawned.GetSpawnedByAgentId())
		assert.NotEqual(t, writer.GetTaskId(), spawned.GetTaskId(), "an agent spawned on its own starts a task of its own")
		assert.Equal(t, agentv1.AgentRole_AGENT_ROLE_WRITER, spawned.GetRole())
	})

	t.Run("a reviewer runs in its writer's checkout", func(t *testing.T) {
		req := connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:     "read the change",
			Title:      "reviewer",
			WorkflowId: workflowID,
			Task:       writer.TaskId,
		})
		callAs(req, writer.GetId())

		created, err := instance.Agents.CreateAgent(ctx, req)
		require.NoError(t, err)

		reviewer = created.Msg.GetAgent()
		assert.Equal(t, writer.GetTaskId(), reviewer.GetTaskId())
		assert.Equal(t, agentv1.AgentRole_AGENT_ROLE_REVIEWER, reviewer.GetRole())
		assert.Equal(t, writer.GetCwd(), reviewer.GetCwd())
	})

	t.Run("archiving the task takes its reviewer", func(t *testing.T) {
		_, err := instance.Tasks.ArchiveTask(ctx, connect.NewRequest(&taskv1.ArchiveTaskRequest{
			Task: writer.GetTaskId(),
		}))
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"unrelated"}, liveNames(t, instance))
		assert.ElementsMatch(t, []string{"author", "reviewer"}, archivedNames(t, instance))
	})

	t.Run("a reviewer cannot come back before its task", func(t *testing.T) {
		_, err := instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{
			Ids: []string{reviewer.GetId()},
		}))
		assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		assert.ElementsMatch(t, []string{"author", "reviewer"}, archivedNames(t, instance))
	})

	t.Run("restoring the task brings back its writer alone", func(t *testing.T) {
		_, err := instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{
			Ids: []string{writer.GetId()},
		}))
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"author", "unrelated"}, liveNames(t, instance))
		assert.ElementsMatch(t, []string{"reviewer"}, archivedNames(t, instance))
	})

	t.Run("a reviewer comes back once its task is live", func(t *testing.T) {
		_, err := instance.Agents.RestoreAgents(ctx, connect.NewRequest(&agentv1.RestoreAgentsRequest{
			Ids: []string{reviewer.GetId()},
		}))
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"author", "reviewer", "unrelated"}, liveNames(t, instance))
	})

	t.Run("a reviewer is archived on its own", func(t *testing.T) {
		_, err := instance.Agents.ArchiveAgents(ctx, connect.NewRequest(&agentv1.ArchiveAgentsRequest{
			Ids: []string{reviewer.GetId()},
		}))
		require.NoError(t, err)

		assert.ElementsMatch(t, []string{"author", "unrelated"}, liveNames(t, instance))
		assert.ElementsMatch(t, []string{"reviewer"}, archivedNames(t, instance))
	})
}

func liveNames(t *testing.T, instance *komradtest.Instance) []string {
	t.Helper()

	return names(t, instance, false)
}

func archivedNames(t *testing.T, instance *komradtest.Instance) []string {
	t.Helper()

	return names(t, instance, true)
}

func names(t *testing.T, instance *komradtest.Instance, archived bool) []string {
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
