package ui_test

import (
	"slices"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestTasksTree covers a task several agents work on standing over them in the agents list.
func TestTasksTree(t *testing.T) {
	t.Run("every task stands over the agents on it", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		assert.Equal(t, []string{"komrad", "├─ solo", "│  └─ loner", "└─ fix-login", "   ├─ follow-up", "   └─ author"}, readListRows(harness.readFrame()))
	})

	t.Run("a task's record is its goal and the agents on it", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press("j")

		frame := harness.readFrame()
		require.Equal(t, "└─ fix-login", readHoveredRow(frame))
		assert.Contains(t, frame, "Logins work again.")
		assert.Contains(t, frame, "• follow-up")
		assert.Contains(t, frame, "• author")
	})

	t.Run("enter on a task opens its newest agent", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)
		answerResume(harness)

		harness.press("j", "enter")

		harness.assertSentTo(agentv1connect.AgentServiceResumeAgentProcedure, &agentv1.ResumeAgentRequest{Id: "a2"})
	})

	t.Run("the task qualifier keeps the task and the agents on it", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press(slices.Concat([]string{"/", "tab"}, spellKeys("task:fix"), []string{"enter"})...)

		assert.Equal(t, []string{"komrad", "└─ fix-login", "   ├─ follow-up", "   └─ author"}, readListRows(harness.readFrame()))
	})

	t.Run("selecting a task takes only the agents on it", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press("j", "v", "space", "a", "enter")

		harness.assertSentTo(agentv1connect.AgentServiceArchiveAgentsProcedure, &agentv1.ArchiveAgentsRequest{Ids: []string{"a2", "a1"}})
	})

	t.Run("a task's menu is its newest agent's pages and another agent on it", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press("j", "space")

		frame := harness.readFrame()
		assert.Contains(t, frame, "status checks")
		assert.Contains(t, frame, "logs")
		assert.Contains(t, frame, "new agent")
		assert.NotContains(t, frame, "new task")
		assert.NotContains(t, frame, "new workflow")
	})

	t.Run("a new agent from a task's menu joins the task", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)
		answerAgentCreate(harness, "")

		harness.press(slices.Concat([]string{"j", "space", "c"}, spellKeys("review it"), []string{"enter"})...)

		harness.assertSentTo(agentv1connect.AgentServiceCreateAgentProcedure, &agentv1.CreateAgentRequest{
			Prompt:     "review it",
			WorkflowId: "w1",
			Task:       xptr.Ptr("t1"),
		})
	})

	t.Run("the archive on a task's menu archives the task", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press("j", "space", "a", "enter")

		harness.assertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, &taskv1.ArchiveTaskRequest{Task: "t1"})
	})
}

func buildTasks() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "komrad"),
		&taskv1.Task{Id: "t1", WorkflowId: "w1", Slug: "fix-login", Title: "Fix login", Goal: "Logins work again."},
		&taskv1.Task{Id: "t2", WorkflowId: "w1", Slug: "solo", Title: "Solo"},
		newTaskAgent("a1", "author", "t1", 0),
		newTaskAgent("a2", "follow-up", "t1", 5),
		newTaskAgent("a3", "loner", "t2", 10),
	}
}

func newTaskAgent(id, name, taskID string, minute int) *agentv1.Agent {
	slugs := map[string]string{"t1": "fix-login", "t2": "solo"}
	agent := newAgentAt(name, "w1", minute)
	agent.Id = id
	agent.TaskId = xptr.Ptr(taskID)
	agent.TaskSlug = xptr.Ptr(slugs[taskID])

	return agent
}
