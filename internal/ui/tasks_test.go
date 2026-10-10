package ui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
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
		assert.Contains(t, frame, "The session cookie expires early.")
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

	t.Run("selecting a task archives only that task", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press("j", "v", "space", "a", "enter")

		harness.assertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, &taskv1.ArchiveTaskRequest{Task: "t1"})
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

	t.Run("a new agent from a task's menu", func(t *testing.T) {
		tests := []struct {
			name    string
			task    *taskv1.Task
			writer  agentv1.AgentState
			offered bool
			form    string
			want    *agentv1.CreateAgentRequest
		}{
			{
				name:    "is not offered once the task's worktree was removed",
				task:    &taskv1.Task{Id: "t1", WorkflowId: "w1", Slug: "fix-login", Cwd: xptr.Ptr("/worktrees/fix-login"), WorktreeRemoved: true},
				writer:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
				offered: false,
			},
			{
				name:    "on a task with a live writer starts a child of the writer",
				task:    &taskv1.Task{Id: "t1", WorkflowId: "w1", Slug: "fix-login", Cwd: xptr.Ptr("/worktrees/fix-login")},
				writer:  agentv1.AgentState_AGENT_STATE_IDLE,
				offered: true,
				form:    `Child of "author"`,
				want: &agentv1.CreateAgentRequest{
					Prompt:        "review it",
					Harness:       "claude",
					WorkflowId:    "w1",
					ParentAgentId: xptr.Ptr("a1"),
				},
			},
			{
				name:    "on a task with a checkout and no live writer starts the writer in it",
				task:    &taskv1.Task{Id: "t1", WorkflowId: "w1", Slug: "fix-login", Cwd: xptr.Ptr("/worktrees/fix-login")},
				writer:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
				offered: true,
				form:    "Becomes the task's writer",
				want: &agentv1.CreateAgentRequest{
					Prompt:     "review it",
					WorkflowId: "w1",
					Task:       xptr.Ptr("t1"),
				},
			},
			{
				name:    "on a task with no checkout starts its first writer",
				task:    &taskv1.Task{Id: "t1", WorkflowId: "w1", Slug: "fix-login"},
				writer:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
				offered: true,
				form:    "Becomes the task's first writer",
				want: &agentv1.CreateAgentRequest{
					Prompt:     "review it",
					WorkflowId: "w1",
					Task:       xptr.Ptr("t1"),
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				writer := newTaskAgent("a1", "author", "t1", 0)
				writer.Harness, writer.Flags, writer.Role = "claude", "--model opus", agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER
				writer.State = tt.writer
				harness := openAgents(t, newLiveWorkflow("w1", "komrad"), tt.task, writer)
				answerAgentCreate(harness, "")

				harness.press("k", "space")
				menu := strings.Join(readStage(harness.readFrame()), "\n")
				if !tt.offered {
					assert.NotContains(t, menu, "new agent")

					return
				}
				require.Contains(t, menu, "new agent")

				harness.press("c")
				assert.Contains(t, harness.readFrame(), tt.form)
				harness.press(slices.Concat(spellKeys("review it"), []string{"enter"})...)

				harness.assertSentTo(agentv1connect.AgentServiceCreateAgentProcedure, tt.want)
			})
		}
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
		&taskv1.Task{Id: "t1", WorkflowId: "w1", Slug: "fix-login", Title: "Fix login", Goal: "Logins work again.", Notes: "The session cookie expires early.", NotesUpdatedAt: xptr.Ptr("2026-10-10T10:00:00.5Z")},
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

// TestTasksRow covers what is done to the work from a task's row, and what is left on an agent's.
func TestTasksRow(t *testing.T) {
	t.Run("r renames the task's slug", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"enter"})...)

		harness.assertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure, &taskv1.UpdateTaskRequest{Task: "t1", Slug: xptr.Ptr("fix-login-v2")})
	})

	t.Run("r inside the task's menu is the review", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press("j", "space", "r")

		assert.Contains(t, harness.readFrame(), "Could not open the review of fix")
		harness.assertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure)
	})

	t.Run("editing the task saves its title and goal", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press(slices.Concat([]string{"j", "space", "E"}, spellKeys(" now"), []string{"tab", "ctrl+e"})...)
		harness.send(harness.panel.NewFieldEdited("goal", "Logins work.\n"))
		harness.press("enter")

		harness.assertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure, &taskv1.UpdateTaskRequest{
			Task:  "t1",
			Title: xptr.Ptr("Fix login now"),
			Goal:  xptr.Ptr("Logins work."),
		})
	})

	t.Run("a blank title keeps the task's form open", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press(slices.Concat([]string{"j", "space", "E"}, slices.Repeat([]string{"backspace"}, len("Fix login")), []string{"enter"})...)

		frame := harness.readFrame()
		assert.Contains(t, frame, `Task "fix-login"`)
		assert.Contains(t, frame, "Logins work again.")
		assert.Contains(t, frame, "a task needs a title")
		assert.NotContains(t, frame, "Could not save task")
		harness.assertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure)
	})

	t.Run("editing the task's notes saves them over the version they were read at", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press("j", "space", "E", "tab", "tab", "ctrl+e")
		harness.send(harness.panel.NewFieldEdited("notes", "Fixed.\n"))
		harness.press("enter")

		harness.assertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure, &taskv1.UpdateTaskRequest{
			Task:           "t1",
			Title:          xptr.Ptr("Fix login"),
			Goal:           xptr.Ptr("Logins work again."),
			Notes:          xptr.Ptr("Fixed."),
			NotesUpdatedAt: xptr.Ptr("2026-10-10T10:00:00.5Z"),
		})
	})

	t.Run("a hand off whose new writer fails to start restores the old one", func(t *testing.T) {
		harness := openAgents(t, buildHandOff()...)
		harness.refuse(agentv1connect.AgentServiceCreateAgentProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no harness named cluade")))

		harness.press("k", "space", "o", "enter")

		harness.assertSentTo(agentv1connect.AgentServiceRestoreAgentsProcedure, &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}})
		assert.Contains(t, harness.readFrame(), "Could not hand off task")
	})

	t.Run("hand off replaces the writer under the task's row in the same checkout", func(t *testing.T) {
		harness := openAgents(t, buildHandOff()...)
		harness.answer(agentv1connect.AgentServiceCreateAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: &agentv1.Agent{Id: "a4"}}), nil
		})
		answerResume(harness)

		harness.press(slices.Concat([]string{"k", "space", "o"}, spellKeys("finish the cookie fix"), []string{"enter"})...)

		harness.assertSentTo(agentv1connect.AgentServiceArchiveAgentsProcedure, &agentv1.ArchiveAgentsRequest{Ids: []string{"a1"}})
		harness.assertSentTo(agentv1connect.AgentServiceCreateAgentProcedure, &agentv1.CreateAgentRequest{
			Prompt:     "You take this task over from author, who wrote it until now. Read the task's notes and run git status before you change anything.\n\nfinish the cookie fix",
			Harness:    "claude",
			Flags:      "--model opus",
			WorkflowId: "w1",
			Task:       xptr.Ptr("t1"),
		})

		successor := newTaskAgent("a4", "successor", "t1", 20)
		successor.Cwd = "/worktrees/fix-login"
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "komrad"), buildHandOff()[1], successor))
		harness.press("k")
		require.Equal(t, "└─ fix-login", strings.TrimSpace(readHoveredRow(harness.readFrame())))
		harness.press("enter")

		harness.assertSentTo(agentv1connect.AgentServiceResumeAgentProcedure, &agentv1.ResumeAgentRequest{Id: "a4"})
		harness.assertSentTo(worktreev1connect.WorktreeServiceDeleteWorktreesProcedure)
	})

	t.Run("a task's menu holds everything done to the work", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press("j", "space")

		menu := strings.Join(readStage(harness.readFrame()), "\n")
		for _, entry := range []string{"review", "logs", "status checks", "terminal", "new agent", "edit task", "hand off", "edit files", "git ›", "jump ›", "hide ›", "archive ›"} {
			assert.Contains(t, menu, entry)
		}
	})

	t.Run("an agent's menu holds only its session", func(t *testing.T) {
		harness := openAgents(t, buildTasks()...)

		harness.press("j", "j", "space")

		menu := strings.Join(readStage(harness.readFrame()), "\n")
		for _, entry := range []string{"logs", "edit files", "archive ›"} {
			assert.Contains(t, menu, entry)
		}
		for _, entry := range []string{"review", "status checks", "terminal", "new child", "new agent", "git ›", "jump ›", "hide ›", "snooze", "retry launch"} {
			assert.NotContains(t, menu, entry)
		}
	})

	t.Run("an agent that failed to launch offers a retry", func(t *testing.T) {
		rows := buildTasks()
		rows[5].(*agentv1.Agent).State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
		harness := openAgents(t, rows...)

		harness.press("space")

		assert.Contains(t, strings.Join(readStage(harness.readFrame()), "\n"), "retry launch")
	})

	t.Run("a task's row shows the pane of an agent on it with one over a newer agent without", func(t *testing.T) {
		rows := buildTasks()
		writer := rows[3].(*agentv1.Agent)
		writer.PaneId = "%3"
		writer.Alive = xptr.Ptr(true)
		harness := openAgents(t, rows...)

		harness.press("j")
		harness.fireTimers()

		assert.Equal(t, "%3", harness.panel.ReadGuestPane())
	})
}

func buildHandOff() []proto.Message {
	writer := newTaskAgent("a1", "author", "t1", 0)
	writer.Harness, writer.Flags, writer.Role = "claude", "--model opus", agentv1.AgentRole_AGENT_ROLE_IMPLEMENTER
	writer.Cwd = "/worktrees/fix-login"
	child := newTaskAgent("a2", "helper", "t1", 5)
	child.ParentAgentId = xptr.Ptr("a1")

	return []proto.Message{
		newLiveWorkflow("w1", "komrad"),
		&taskv1.Task{Id: "t1", WorkflowId: "w1", Slug: "fix-login", Title: "Fix login", Goal: "Logins work again.", Cwd: xptr.Ptr("/worktrees/fix-login"), Branch: xptr.Ptr("fix-login")},
		child,
		writer,
	}
}
