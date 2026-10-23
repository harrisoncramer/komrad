package ui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	watchv1 "komrad/proto/gen/watch/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestTasksTree covers a task several agents work on standing over them in the agents list.
func TestTasksTree(t *testing.T) {
	t.Run("every task stands over the agents on it", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		assert.Equal(t, []string{"komrad", "├─ solo", "└─ fix-login", "   └─ " + reviewerMark + "follow-up"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("enter on a task opens its writer", func(t *testing.T) {
		for _, press := range [][]string{{"j", "enter"}, {"j", "space", "n", "enter"}} {
			harness := paneltest.OpenAgents(t, buildTasks()...)
			harness.AnswerResume()

			harness.Press(press...)

			harness.AssertSentTo(agentv1connect.AgentServiceResumeAgentProcedure, &agentv1.ResumeAgentRequest{Id: "a1"})
		}
	})

	t.Run("the task qualifier keeps the task and the agents on it", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.Press(slices.Concat([]string{"/", "tab"}, strings.Split("task:fix", ""), []string{"enter"})...)

		assert.Equal(t, []string{"komrad", "└─ fix-login", "   └─ " + reviewerMark + "follow-up"}, paneltest.ListRows(harness.Frame()))
	})

	t.Run("selecting a task archives only that task", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.Press("j", "v", "space", "a", "enter")

		harness.AssertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, &taskv1.ArchiveTaskRequest{Task: &taskv1.ArchiveTaskRequest_TaskId{TaskId: "t1"}})
	})

	t.Run("a task's menu is its writer's pages and a new agent on it, over the list's new workflow", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.Press("j", "space")

		frame := harness.Frame()
		assert.Contains(t, frame, "status checks")
		assert.Contains(t, frame, "logs")
		assert.Contains(t, frame, "new agent")
		assert.NotContains(t, frame, "new task")
		assert.Contains(t, frame, "new workflow")
	})

	t.Run("a new agent or a restart from a task's menu", func(t *testing.T) {
		tests := []struct {
			name       string
			task       *taskv1.Task
			writer     agentv1.AgentState
			writerless bool
			deleting   bool
			offered    bool
			form       string
			want       *agentv1.CreateAgentRequest
		}{
			{
				name: "is not offered once the task's worktree was removed",
				task: &taskv1.Task{
					Id:                    "t1",
					WorkflowId:            "w1",
					Slug:                  "fix-login",
					Cwd:                   xptr.Ptr("/worktrees/fix-login"),
					WorktreeRemovalReason: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE,
				},
				writer:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
				offered: false,
			},
			{
				name: "is not offered on an archived task with no checkout",
				task: &taskv1.Task{
					Id:         "t1",
					WorkflowId: "w1",
					Slug:       "fix-login",
					ArchivedAt: timestamppb.New(time.Date(2026, 10, 10, 10, 0, 0, 0, time.UTC)),
				},
				writer:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
				offered: false,
			},
			{
				name: "is not offered once the task is being deleted",
				task: &taskv1.Task{
					Id:         "t1",
					WorkflowId: "w1",
					Slug:       "fix-login",
					Cwd:        xptr.Ptr("/worktrees/fix-login"),
				},
				writer:   agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
				deleting: true,
				offered:  false,
			},
			{
				name: "on a task with a live writer starts a reviewer in the writer's checkout",
				task: &taskv1.Task{
					Id:         "t1",
					WorkflowId: "w1",
					Slug:       "fix-login",
					Cwd:        xptr.Ptr("/worktrees/fix-login"),
				},
				writer:  agentv1.AgentState_AGENT_STATE_IDLE,
				offered: true,
				form:    `Reviewer on "fix-login"`,
				want: &agentv1.CreateAgentRequest{
					Prompt:     "review it",
					Harness:    "claude",
					WorkflowId: "w1",
					Task:       &agentv1.CreateAgentRequest_TaskId{TaskId: "t1"},
				},
			},
			{
				name: "on a task whose writer failed to start starts a reviewer in its checkout",
				task: &taskv1.Task{
					Id:         "t1",
					WorkflowId: "w1",
					Slug:       "fix-login",
					Cwd:        xptr.Ptr("/worktrees/fix-login"),
				},
				writer:  agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
				offered: true,
				form:    `Reviewer on "fix-login"`,
				want: &agentv1.CreateAgentRequest{
					Prompt:     "review it",
					Harness:    "claude",
					WorkflowId: "w1",
					Task:       &agentv1.CreateAgentRequest_TaskId{TaskId: "t1"},
				},
			},
			{
				name: "on a task with a checkout and no writer starts the writer in it",
				task: &taskv1.Task{
					Id:         "t1",
					WorkflowId: "w1",
					Slug:       "fix-login",
					Cwd:        xptr.Ptr("/worktrees/fix-login"),
				},
				writerless: true,
				offered:    true,
				form:       "The agent becomes the task's writer",
				want: &agentv1.CreateAgentRequest{
					Prompt:     "review it",
					WorkflowId: "w1",
					Task:       &agentv1.CreateAgentRequest_TaskId{TaskId: "t1"},
				},
			},
			{
				name: "on a task with no checkout starts its first writer",
				task: &taskv1.Task{
					Id:         "t1",
					WorkflowId: "w1",
					Slug:       "fix-login",
				},
				writerless: true,
				offered:    true,
				form:       "The agent becomes the task's first writer",
				want: &agentv1.CreateAgentRequest{
					Prompt:     "review it",
					WorkflowId: "w1",
					Task:       &agentv1.CreateAgentRequest_TaskId{TaskId: "t1"},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				writer := newTaskAgent("a1", "author", "t1", agentv1.AgentRole_AGENT_ROLE_WRITER, 0)
				writer.Harness, writer.Flags = "claude", "--model opus"
				writer.State = tt.writer
				onTask := writer
				if tt.writerless {
					onTask = newTaskAgent("a2", "reviewer", "t1", agentv1.AgentRole_AGENT_ROLE_REVIEWER, 0)
				}
				harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "komrad"), tt.task, onTask)
				answerAgentCreate(harness, "")
				if tt.deleting {
					harness.Watch(paneltest.Deleted(watchv1.Resource_RESOURCE_TASK, "t1"))
				}

				harness.Press("space")
				menu := strings.Join(paneltest.Stage(harness.Frame()), "\n")
				require.Contains(t, menu, "edit task")
				if !tt.offered {
					assert.NotContains(t, menu, "new agent")
					assert.NotContains(t, menu, "restart")

					return
				}
				require.Contains(t, menu, "new agent")

				harness.Press("c")
				assert.Contains(t, harness.Frame(), tt.form)
				harness.Press(slices.Concat(strings.Split("review it", ""), []string{"enter"})...)

				harness.AssertSentTo(agentv1connect.AgentServiceCreateAgentProcedure, tt.want)
			})
		}
	})

	t.Run("the archive on a task's menu archives the task", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.Press("j", "space", "a", "enter")

		harness.AssertSentTo(taskv1connect.TaskServiceArchiveTaskProcedure, &taskv1.ArchiveTaskRequest{Task: &taskv1.ArchiveTaskRequest_TaskId{TaskId: "t1"}})
	})
}

func buildTasks() []proto.Message {
	return []proto.Message{
		paneltest.Workflow("w1", "komrad"),
		&taskv1.Task{
			Id:             "t1",
			WorkflowId:     "w1",
			Slug:           "fix-login",
			Title:          "Fix login",
			Goal:           "Logins work again.",
			Notes:          "The session cookie expires early.",
			NotesUpdatedAt: timestamppb.New(time.Date(2026, 10, 10, 10, 0, 0, 500_000_000, time.UTC)),
		},
		&taskv1.Task{
			Id:         "t2",
			WorkflowId: "w1",
			Slug:       "solo",
			Title:      "Solo",
		},
		newTaskAgent("a1", "author", "t1", agentv1.AgentRole_AGENT_ROLE_WRITER, 0),
		newTaskAgent("a2", "follow-up", "t1", agentv1.AgentRole_AGENT_ROLE_REVIEWER, 5),
		newTaskAgent("a3", "loner", "t2", agentv1.AgentRole_AGENT_ROLE_WRITER, 10),
	}
}

func newTaskAgent(id, name, taskID string, role agentv1.AgentRole, minute int) *agentv1.Agent {
	slugs := map[string]string{"t1": "fix-login", "t2": "solo"}
	agent := paneltest.AgentAt(name, "w1", minute)
	agent.Id = id
	agent.Role = role
	agent.TaskId = xptr.Ptr(taskID)
	agent.TaskSlug = xptr.Ptr(slugs[taskID])

	return agent
}

// TestTasksRow covers what is done to the work from a task's row, and what is left on an agent's.
func TestTasksRow(t *testing.T) {
	t.Run("r renames the task's slug", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.Press(slices.Concat([]string{"j", "r"}, strings.Split("-v2", ""), []string{"enter"})...)

		harness.AssertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure, &taskv1.UpdateTaskRequest{Task: &taskv1.UpdateTaskRequest_TaskId{TaskId: "t1"}, Slug: xptr.Ptr("fix-login-v2")})
	})

	t.Run("r inside the task's menu is the review", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.Press("j", "space", "r")

		assert.Contains(t, harness.Frame(), "Could not open the review of fix")
		harness.AssertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure)
	})

	t.Run("editing the task saves its title and goal", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.WriteInEditor("Logins work.\n")
		harness.Press(slices.Concat([]string{"j", "space", "E"}, strings.Split(" now", ""), []string{"tab", "ctrl+e"})...)
		harness.Press("enter")

		harness.AssertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure, &taskv1.UpdateTaskRequest{
			Task:  &taskv1.UpdateTaskRequest_TaskId{TaskId: "t1"},
			Title: xptr.Ptr("Fix login now"),
			Goal:  xptr.Ptr("Logins work."),
		})
	})

	t.Run("a blank title keeps the task's form open", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.Press(slices.Concat([]string{"j", "space", "E"}, slices.Repeat([]string{"backspace"}, len("Fix login")), []string{"enter"})...)

		frame := harness.Frame()
		assert.Contains(t, frame, `Task "fix-login"`)
		assert.Contains(t, frame, "Logins work again.")
		assert.Contains(t, frame, "a task needs a title")
		assert.NotContains(t, frame, "Could not save task")
		harness.AssertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure)
	})

	t.Run("editing the task's notes saves them over the version they were read at", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.WriteInEditor("Fixed.\n")
		harness.Press("j", "space", "E", "tab", "tab", "ctrl+e")
		harness.Press("enter")

		harness.AssertSentTo(taskv1connect.TaskServiceUpdateTaskProcedure, &taskv1.UpdateTaskRequest{
			Task:           &taskv1.UpdateTaskRequest_TaskId{TaskId: "t1"},
			Title:          xptr.Ptr("Fix login"),
			Goal:           xptr.Ptr("Logins work again."),
			Notes:          xptr.Ptr("Fixed."),
			NotesUpdatedAt: timestamppb.New(time.Date(2026, 10, 10, 10, 0, 0, 500_000_000, time.UTC)),
		})
	})

	t.Run("a restart that fails says so", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildRestart()...)
		harness.Refuse(agentv1connect.AgentServiceRestartAgentProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("restore its task first")))

		harness.Press("space", "o", "enter")

		assert.Contains(t, harness.Frame(), "Could not restart agent")
	})

	t.Run("a restart of a writer that never started says to retry it", func(t *testing.T) {
		rows := buildRestart()
		writer, _ := rows[3].(*agentv1.Agent)
		writer.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
		harness := paneltest.OpenAgents(t, rows...)

		harness.Press("space", "o")

		assert.Contains(t, harness.ReadMessages(), "writer never started, so retry it")
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceRestartAgentProcedure))
	})

	t.Run("a restart relaunches the writer with the note", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildRestart()...)
		harness.Answer(agentv1connect.AgentServiceRestartAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.RestartAgentResponse{Agent: &agentv1.Agent{Id: "a1"}}), nil
		})

		harness.Press(slices.Concat([]string{"space", "o"}, strings.Split("finish the cookie fix", ""), []string{"enter"})...)

		harness.AssertSentTo(agentv1connect.AgentServiceRestartAgentProcedure, &agentv1.RestartAgentRequest{
			Id:      "a1",
			Note:    "finish the cookie fix",
			Harness: "claude",
			Flags:   "--model opus",
		})
	})

	t.Run("a restart moves the writer onto another CLI and model", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildRestart()...)
		harness.Answer(agentv1connect.AgentServiceRestartAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.RestartAgentResponse{Agent: &agentv1.Agent{Id: "a1"}}), nil
		})

		harness.Press(slices.Concat([]string{"space", "o", "tab", "ctrl+u"}, strings.Split("codex", ""), []string{"tab", "ctrl+u"}, strings.Split("--model o3", ""), []string{"enter"})...)

		harness.AssertSentTo(agentv1connect.AgentServiceRestartAgentProcedure, &agentv1.RestartAgentRequest{
			Id:      "a1",
			Harness: "codex",
			Flags:   "--model o3",
		})
	})

	t.Run("a task's menu holds everything done to the work", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.Press("j", "space")

		menu := strings.Join(paneltest.Stage(harness.Frame()), "\n")
		for _, entry := range []string{"review", "logs", "status checks", "terminal", "new agent", "edit task", "notes", "restart", "edit files", "git ›", "jump ›", "hide ›", "archive ›"} {
			assert.Contains(t, menu, entry)
		}
	})

	t.Run("an agent's menu holds only its session", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)

		harness.Press("j", "j", "j", "space")

		menu := strings.Join(paneltest.Stage(harness.Frame()), "\n")
		for _, entry := range []string{"logs", "edit files", "archive ›"} {
			assert.Contains(t, menu, entry)
		}
		for _, entry := range []string{"review", "status checks", "terminal", "new agent", "git ›", "jump ›", "hide ›", "snooze", "retry launch"} {
			assert.NotContains(t, menu, entry)
		}
	})

	t.Run("an agent that failed to launch offers a retry", func(t *testing.T) {
		rows := buildTasks()
		rows[5].(*agentv1.Agent).State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
		harness := paneltest.OpenAgents(t, rows...)

		harness.Press("space")

		assert.Contains(t, strings.Join(paneltest.Stage(harness.Frame()), "\n"), "retry launch")
	})

	t.Run("a task's row shows its writer's pane, and its notes in place of it until escape", func(t *testing.T) {
		rows := buildTasks()
		writer := rows[3].(*agentv1.Agent)
		writer.PaneId = "%3"
		writer.Alive = xptr.Ptr(true)
		harness := paneltest.OpenAgents(t, rows...)

		harness.Press("j")
		harness.Advance(time.Second)
		require.Equal(t, "%3", harness.Tmux().GuestPane())
		require.False(t, harness.Tmux().IsZoomed())

		harness.Press("space", "n")
		harness.Advance(time.Second)

		assert.True(t, harness.Tmux().IsZoomed())
		stage := paneltest.StageText(harness.Frame())
		for _, text := range []string{"Fix login", "Logins work again.", "The session cookie expires early."} {
			assert.Contains(t, stage, text)
		}
		assert.NotContains(t, stage, "• follow-up")

		harness.Press("esc")
		harness.Advance(time.Second)

		assert.False(t, harness.Tmux().IsZoomed())
	})

	t.Run("a says: search that finds the writer leaves its task's notes to show", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildTasks()...)
		harness.Answer(agentv1connect.AgentServiceSearchTranscriptsProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.SearchTranscriptsResponse{Matches: []*agentv1.SearchHit{{AgentId: "a1", Snippet: "\x02cookie\x03"}}}), nil
		})
		harness.Press(slices.Concat([]string{"/"}, strings.Split("says:cookie", ""), []string{"enter"})...)
		harness.Advance(time.Second)
		require.Contains(t, paneltest.HoveredRow(harness.Frame()), "fix-login")

		harness.Press("space", "n")
		harness.Advance(time.Second)

		assert.Contains(t, paneltest.StageText(harness.Frame()), "The session cookie expires early.")
	})
}

func buildRestart() []proto.Message {
	writer := newTaskAgent("a1", "author", "t1", agentv1.AgentRole_AGENT_ROLE_WRITER, 0)
	writer.Harness, writer.Flags = "claude", "--model opus"
	writer.Cwd = "/worktrees/fix-login"
	reviewer := newTaskAgent("a2", "reviewer", "t1", agentv1.AgentRole_AGENT_ROLE_REVIEWER, 5)

	return []proto.Message{
		paneltest.Workflow("w1", "komrad"),
		&taskv1.Task{
			Id:         "t1",
			WorkflowId: "w1",
			Slug:       "fix-login",
			Title:      "Fix login",
			Goal:       "Logins work again.",
			Cwd:        xptr.Ptr("/worktrees/fix-login"),
			Branch:     xptr.Ptr("fix-login"),
		},
		reviewer,
		writer,
	}
}
