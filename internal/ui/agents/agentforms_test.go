package agents_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

// TestFormsSubmit covers every form sending what the user typed into it and closing once the write lands.
func TestFormsSubmit(t *testing.T) {
	tests := []struct {
		name      string
		workflow  *workflowv1.Workflow
		author    *agentv1.Agent
		empty     bool
		hostPath  string
		press     []string
		answer    connect.AnyResponse
		settle    []*watchv1.WatchEvent
		procedure string
		want      proto.Message
	}{
		{
			name:      "the new-task form starts its agent and its task with an overridden harness",
			press:     slices.Concat([]string{"space", "c"}, strings.Split("run the checks", ""), []string{"tab", "ctrl+u"}, strings.Split("codex", ""), []string{"enter"}),
			answer:    connect.NewResponse(&agentv1.CreateAgentResponse{Agent: &agentv1.Agent{Id: "a2"}}),
			settle:    []*watchv1.WatchEvent{paneltest.Puts(paneltest.Agent("a2", "ci", "w1"))},
			procedure: agentv1connect.AgentServiceCreateAgentProcedure,
			want: &agentv1.CreateAgentRequest{
				Prompt:     "run the checks",
				Harness:    "codex",
				WorkflowId: "w1",
			},
		},
		{
			name: "the adopt form sends the ref and prompt into the workflow",
			press: slices.Concat([]string{"space", "A"}, strings.Split("https://github.com/acmecorp/admin/pull/1069", ""), []string{"tab"},
				strings.Split("pick it up", ""), []string{"enter"}),
			answer:    connect.NewResponse(&agentv1.AdoptResponse{Agent: &agentv1.Agent{Id: "a2"}}),
			settle:    []*watchv1.WatchEvent{paneltest.Puts(paneltest.Agent("a2", "adopted", "w1"))},
			procedure: agentv1connect.AgentServiceAdoptProcedure,
			want: &agentv1.AdoptRequest{
				Ref:        "https://github.com/acmecorp/admin/pull/1069",
				Prompt:     "pick it up",
				Harness:    "claude",
				WorkflowId: "w1",
			},
		},
		{
			name:      "the first new-workflow form starts where tmux was invoked from",
			empty:     true,
			hostPath:  "/work/from-tmux",
			press:     slices.Concat([]string{"space", "w"}, strings.Split("web", ""), []string{"enter"}),
			answer:    connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}),
			settle:    []*watchv1.WatchEvent{paneltest.Puts(paneltest.Workflow("w2", "web"))},
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			want: &workflowv1.CreateWorkflowRequest{
				Name:     "web",
				Cwd:      "/work/from-tmux",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
		},
		{
			name: "the new-workflow form with no provisioner makes no worktrees",
			press: slices.Concat([]string{"space", "w"}, strings.Split("web", ""), []string{"tab"}, strings.Split("the frontend", ""), []string{"tab"},
				strings.Split("/work/web", ""), []string{"tab", "up", "tab", "enter", "tab"}, strings.Split("--model sonnet", ""), []string{"tab"}, strings.Split("codex", ""),
				[]string{"tab"}, strings.Split("--model gpt-5", ""), []string{"enter"}),
			answer:    connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}),
			settle:    []*watchv1.WatchEvent{paneltest.Puts(paneltest.Workflow("w2", "web"))},
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			want: &workflowv1.CreateWorkflowRequest{
				Name:            "web",
				Context:         "the frontend",
				Cwd:             "/work/web",
				Flags:           "--model sonnet",
				ReviewerHarness: "codex",
				ReviewerFlags:   "--model gpt-5",
				Worktree:        &workflowv1.WorktreeConfig{},
			},
		},
		{
			name: "the new-workflow form with worktrees sends each path list without its blank lines and the setup command",
			press: slices.Concat([]string{"space", "w"}, strings.Split("web", ""), openWorkflowSubformKeys(1),
				strings.Split("develop", ""), []string{"tab", "tab"}, strings.Split(".env", ""), []string{"ctrl+j", "ctrl+j"}, strings.Split("config/local.json", ""),
				[]string{"tab"}, strings.Split("node_modules", ""), []string{"tab"}, strings.Split("bin/setup", ""), []string{"enter"}),
			answer:    connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}),
			settle:    []*watchv1.WatchEvent{paneltest.Puts(paneltest.Workflow("w2", "web"))},
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			want: &workflowv1.CreateWorkflowRequest{
				Name: "web",
				Worktree: &workflowv1.WorktreeConfig{
					Enabled:      true,
					BaseBranch:   "develop",
					CopyPaths:    []string{".env", "config/local.json"},
					SymlinkPaths: []string{"node_modules"},
					SetupCommand: "bin/setup",
				},
			},
		},
		{
			name:      "the workflow edit form sends the field edited and the ones that were not",
			workflow:  newConfiguredWorkflow(),
			press:     slices.Concat([]string{"space", "E", "tab", "tab", "ctrl+u"}, strings.Split("/work/api-v2", ""), []string{"enter"}),
			procedure: workflowv1connect.WorkflowServiceUpdateWorkflowProcedure,
			want: &workflowv1.UpdateWorkflowRequest{
				Id:       "w1",
				Name:     "api",
				Context:  "shared notes",
				Cwd:      "/work/api-v2",
				Flags:    "--model opus",
				Harness:  "claude",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
		},
		{
			name:  "the workflow archive prompt archives the workflow",
			press: []string{"space", "a", "enter"},
			settle: []*watchv1.WatchEvent{
				paneltest.Deleted(watchv1.Resource_RESOURCE_WORKFLOW, "w1"),
				paneltest.Deleted(watchv1.Resource_RESOURCE_AGENT, "a1"),
			},
			procedure: workflowv1connect.WorkflowServiceArchiveWorkflowProcedure,
			want:      &workflowv1.ArchiveWorkflowRequest{Id: "w1"},
		},
		{
			name:   "the workflow archive prompt archives the workflow from its orchestrator's menu",
			author: paneltest.Orchestrator(),
			press:  []string{"space", "a", "enter"},
			settle: []*watchv1.WatchEvent{
				paneltest.Deleted(watchv1.Resource_RESOURCE_WORKFLOW, "w1"),
				paneltest.Deleted(watchv1.Resource_RESOURCE_AGENT, "o1"),
			},
			procedure: workflowv1connect.WorkflowServiceArchiveWorkflowProcedure,
			want:      &workflowv1.ArchiveWorkflowRequest{Id: "w1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := tt.workflow
			if workflow == nil {
				workflow = newFormWorkflow()
			}
			author := tt.author
			if author == nil {
				author = newFormAuthor()
			}
			var harness *paneltest.Panel
			if tt.empty {
				harness = paneltest.OpenAgents(t)
			} else {
				harness = openForms(t, workflow, author)
			}
			harness.Tmux().SetHostPath(tt.hostPath)
			if tt.answer != nil {
				harness.Answer(tt.procedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return tt.answer, nil
				})
			}

			harness.Press(tt.press...)
			if len(tt.settle) > 0 {
				harness.Watch(tt.settle...)
			}

			harness.AssertSentToWith(tt.procedure, ignoringEventPrompts, tt.want)
			assert.NotContains(t, harness.Frame(), "Could not", "the server accepted the write")
			assert.NotContains(t, harness.Frame(), "enter submit", "and the form it closed is gone")
		})
	}
}

// TestFormsNewAgentWorktree covers the one thing an agent can say for itself about its worktree.
func TestFormsNewAgentWorktree(t *testing.T) {
	workflow := newFormWorkflow()
	workflow.Worktree = &workflowv1.WorktreeConfig{Enabled: true}

	t.Run("the field opens on what the workflow does", func(t *testing.T) {
		harness := openForms(t, workflow, newFormAuthor())

		harness.Press("space", "c")

		assert.Contains(t, harness.Frame(), "› Same as workflow")
	})

	tests := []struct {
		name  string
		press []string
		want  *bool
	}{
		{
			name: "left on the default nothing is sent",
		},
		{
			name:  "choosing none is sent as an override rather than as nothing",
			press: []string{"tab", "tab", "tab", "tab", "tab", "down"},
			want:  xptr.Ptr(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openForms(t, workflow, newFormAuthor())

			harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("go", ""), tt.press, []string{"enter"})...)

			sent := harness.Sent(agentv1connect.AgentServiceCreateAgentProcedure)
			require.Len(t, sent, 1)
			create, ok := sent[0].(*agentv1.CreateAgentRequest)
			require.True(t, ok)
			assert.Equal(t, tt.want, create.ProvisionEnabled)
		})
	}
}

// TestFormsRefused covers a refused write leaving the form up, holding what the user typed.
func TestFormsRefused(t *testing.T) {
	tests := []struct {
		name      string
		procedure string
		press     []string
		typed     string
	}{
		{
			name:      "the new-agent form",
			procedure: agentv1connect.AgentServiceCreateAgentProcedure,
			press:     slices.Concat([]string{"space", "c"}, strings.Split("check CI", ""), []string{"enter"}),
			typed:     "check CI",
		},
		{
			name:      "the new-workflow form",
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			press:     slices.Concat([]string{"space", "w"}, strings.Split("web", ""), []string{"enter"}),
			typed:     "web",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openForms(t, newFormWorkflow(), newFormAuthor())
			harness.Refuse(tt.procedure, connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed")))

			harness.Press(tt.press...)

			assert.Contains(t, harness.Frame(), tt.typed, "the form is still up holding what was filled in")
			assert.Contains(t, harness.Frame(), "not allowed", "the reason is rendered under the form")

			harness.Press("ctrl+g")
			frame := harness.Frame()
			assert.Contains(t, frame, "not allowed", "a refused write is reported")
			assert.Contains(t, frame, "fix and submit again", "ctrl+g opens the whole refusal over the form")

			harness.Press("ctrl+g", "!")
			assert.Contains(t, harness.Frame(), tt.typed+"!", "and the form takes keys again")
		})
	}
}

// TestFormsAdoptExisting covers an adoption answered with the agent already on the branch.
func TestFormsAdoptExisting(t *testing.T) {
	tucked := paneltest.Agent("a4", "tucked", "w1")
	tuckedTask := paneltest.TaskOf(tucked)
	tuckedTask.Hidden = true
	reviewer := paneltest.Reviewer("a5", "reviewer", "a4")

	tests := []struct {
		name        string
		existing    *agentv1.Agent
		wantScreen  screen.ID
		wantHovered string
	}{
		{
			name:        "a live agent lands the agents list on its row",
			existing:    newFormAuthor(),
			wantScreen:  screen.Agents,
			wantHovered: "author",
		},
		{
			name:        "a writer on a hidden task shows the hidden tasks and lands on its task's row",
			existing:    tucked,
			wantScreen:  screen.Agents,
			wantHovered: "task-a4",
		},
		{
			name:        "a reviewer on a hidden task shows the hidden tasks and lands on its row",
			existing:    reviewer,
			wantScreen:  screen.Agents,
			wantHovered: "reviewer",
		},
		{
			name:        "an archived agent lands the archive on its row",
			existing:    paneltest.ArchivedAgent("a3", "old-work", "w1"),
			wantScreen:  screen.Agents,
			wantHovered: "old-work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenAgents(t, newFormWorkflow(), newFormAuthor(), paneltest.ArchivedAgent("a3", "old-work", "w1"), tucked, tuckedTask, reviewer)
			harness.Press("g", "g")
			harness.Answer(agentv1connect.AgentServiceAdoptProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.AdoptResponse{Agent: tt.existing, Existing: true}), nil
			})

			harness.Press(slices.Concat([]string{"space", "A"}, strings.Split("someones/work", ""), []string{"enter"})...)

			frame := harness.Frame()
			assert.Equal(t, tt.wantScreen, paneltest.Screen(harness.Frame()))
			assert.Contains(t, paneltest.HoveredRow(frame), tt.wantHovered)
			assert.Contains(t, frame, "This agent already exists")
			assert.NotContains(t, frame, "Could not adopt")
		})
	}
}

// TestFormsInFlight covers the new-agent form while its create has not come back.
func TestFormsInFlight(t *testing.T) {
	create := agentv1connect.AgentServiceCreateAgentProcedure
	submit := slices.Concat([]string{"space", "c"}, strings.Split("check CI", ""), []string{"enter"})

	t.Run("the form stays up saying it is submitting until the create lands", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		answerAgentCreate(harness, "")
		harness.Hold(create)

		harness.Press(submit...)

		frame := harness.Frame()
		assert.Contains(t, frame, "starting the agent…")
		assert.Contains(t, frame, "check CI", "the form is still up")
		assert.Empty(t, harness.Tmux().Respawns(), "no pane is opened for the provisioner")

		harness.Release(create)

		assert.Contains(t, harness.Frame(), "check CI", "the form waits for the row the create made")

		harness.Watch(paneltest.Puts(paneltest.Agent("a2", "check-ci", "w1")))

		frame = harness.Frame()
		assert.NotContains(t, frame, "starting the agent…")
		assert.NotContains(t, frame, "check CI", "the form goes once the row is shown")
	})

	t.Run("the form keeps saying so while the pane the create made warms up", func(t *testing.T) {
		frames := &paneltest.HarnessFrames{}
		harness := paneltest.Start(t, paneltest.Params{HarnessFrames: frames})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(newFormWorkflow(), newFormAuthor()))
		answerAgentCreate(harness, "%9")
		harness.Press("g", "g")

		harness.Press(submit...)

		frame := harness.Frame()
		assert.Contains(t, frame, "starting the agent…")
		assert.Contains(t, frame, "check CI", "the form is still up")

		harness.Watch(paneltest.Puts(newCreatedAgent()))
		assert.Contains(t, harness.Frame(), "check CI", "the row alone does not close it")

		frames.IsShowing = true
		harness.Advance(time.Second)

		frame = harness.Frame()
		assert.NotContains(t, frame, "starting the agent…")
		assert.NotContains(t, frame, "check CI", "the form goes with the reveal")
		assert.Equal(t, "%9", harness.Tmux().GuestPane())
	})

	t.Run("a row that lands before its create comes back takes the cursor with the create", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		answerAgentCreate(harness, "")
		harness.Hold(create)
		harness.Press(submit...)

		harness.Watch(paneltest.Puts(paneltest.Agent("a2", "check-ci", "w1")))
		harness.Release(create)

		frame := harness.Frame()
		assert.NotContains(t, frame, "check CI", "the form goes once the create lands")
		assert.Contains(t, paneltest.HoveredRow(frame), "check-ci")
		assert.Contains(t, paneltest.Trail(frame), "check-ci")
	})

	t.Run("a second submit sends nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Hold(create)
		harness.Press(submit...)

		harness.Press("enter", "!")

		assert.Len(t, harness.Sent(create), 1, "the key never reached the submit path")
		assert.Contains(t, harness.Frame(), "check CI", "and the form is as it was")
		assert.NotContains(t, harness.Frame(), "check CI!")
	})

	t.Run("esc hands the panel back and the create still lands without taking the cursor", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		answerAgentCreate(harness, "")
		harness.Hold(create)
		harness.Press(submit...)

		harness.Press("esc")

		assert.NotContains(t, harness.Frame(), "what the agent is told", "the form it was holding is gone")
		assert.Contains(t, paneltest.Legend(harness.Frame()), "⠋ starting", "the create it left is rendered in the corner")
		harness.Press("j")
		assert.Equal(t, "└─ author", paneltest.HoveredRow(harness.Frame()), "the panel takes keys again")

		harness.Release(create)
		harness.Watch(paneltest.Puts(paneltest.Agent("a2", "check-ci", "w1")))

		frame := harness.Frame()
		assert.Equal(t, "\uf444 local", paneltest.EngineLine(frame), "nothing is reported")
		assert.Equal(t, "└─ author", paneltest.HoveredRow(frame), "the cursor stays where the reader left it")
	})

	t.Run("an escaped create landing leaves a later form held", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Hold(create)
		harness.Press(submit...)
		harness.Press("esc")
		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("the second one", ""), []string{"enter"})...)

		harness.Release(create)

		frame := harness.Frame()
		assert.Contains(t, frame, "starting the agent…", "the second form is still held")
		assert.Contains(t, frame, "the second one")
	})
}

// TestFormsArchivePrompt covers the ways the agent and task archive menus can be answered.
func TestFormsArchivePrompt(t *testing.T) {
	t.Run("an agent's menu archives that one agent and nothing more", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press("j", "space", "a")

		frame := harness.Frame()
		assert.Contains(t, frame, "› archive this agent")
		assert.Contains(t, frame, "back")
		assert.NotContains(t, frame, "for good")
	})

	t.Run("a task's menu opens on the reversible answer", func(t *testing.T) {
		harness := openTaskForms(t, newFormAuthor())

		harness.Press("j", "space", "a")

		frame := harness.Frame()
		assert.Contains(t, frame, "› archive with its agents")
		assert.Contains(t, frame, "archive and delete for good")
		assert.Contains(t, frame, "back")
	})

	t.Run("an agent's archive menu", func(t *testing.T) {
		tests := []struct {
			name     string
			press    []string
			wantSent []paneltest.Request
		}{
			{
				name:  "submitting without moving archives the agent",
				press: []string{"enter"},
				wantSent: []paneltest.Request{{
					Procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
					Msg:       &agentv1.ArchiveAgentsRequest{Ids: []string{"a1"}},
				}},
			},
			{
				name:  "the last row leaves the agent where it is",
				press: []string{"down", "enter"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openForms(t, newFormWorkflow(), newFormAuthor())
				harness.Press("j", "space", "a")
				before := harness.Requests()

				harness.Press(tt.press...)

				harness.AssertSent(append(before, tt.wantSent...)...)
				assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()), "every answer lands back on the list")
			})
		}
	})

	t.Run("a task's archive menu", func(t *testing.T) {
		tests := []struct {
			name     string
			press    []string
			wantSent []paneltest.Request
		}{
			{
				name:  "submitting without moving archives the task and keeps the record",
				press: []string{"enter"},
				wantSent: []paneltest.Request{{
					Procedure: taskv1connect.TaskServiceArchiveTaskProcedure,
					Msg:       &taskv1.ArchiveTaskRequest{Task: &taskv1.ArchiveTaskRequest_TaskId{TaskId: "task-a1"}},
				}},
			},
			{
				name:  "one row down deletes the task once confirmed",
				press: []string{"down", "enter", "enter"},
				wantSent: []paneltest.Request{{
					Procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
					Msg:       &taskv1.DeleteTasksRequest{TaskIds: []string{"task-a1"}, Records: true},
				}},
			},
			{
				name:  "declining the confirm deletes nothing",
				press: []string{"down", "enter", "tab", "n"},
			},
			{
				name:  "the last row leaves the task where it is",
				press: []string{"down", "down", "enter"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openTaskForms(t, newFormAuthor())
				harness.Press("j", "space", "a")
				before := harness.Requests()

				harness.Press(tt.press...)

				harness.AssertSent(append(before, tt.wantSent...)...)
				assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()), "every answer lands back on the list")
			})
		}
	})

	t.Run("a selection's archive menu", func(t *testing.T) {
		siblings := []string{"j", "j", "v", "j", "v"}
		wholeTask := []string{"j", "v"}

		tests := []struct {
			name     string
			choose   []string
			press    []string
			wantSent []paneltest.Request
		}{
			{
				name:   "archiving chosen agents leaves the rest of their task standing",
				choose: siblings,
				press:  []string{"enter"},
				wantSent: []paneltest.Request{{
					Procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
					Msg:       &agentv1.ArchiveAgentsRequest{Ids: []string{"a4", "a3"}},
				}},
			},
			{
				name:   "delete is disabled for chosen agents short of their whole task",
				choose: siblings,
				press:  []string{"down", "enter", "enter"},
			},
			{
				name:   "archiving every agent on a task archives the task",
				choose: wholeTask,
				press:  []string{"enter"},
				wantSent: []paneltest.Request{{
					Procedure: taskv1connect.TaskServiceArchiveTaskProcedure,
					Msg:       &taskv1.ArchiveTaskRequest{Task: &taskv1.ArchiveTaskRequest_TaskId{TaskId: "task-writer"}},
				}},
			},
			{
				name:   "deleting every agent on a task deletes the task",
				choose: wholeTask,
				press:  []string{"down", "enter", "enter"},
				wantSent: []paneltest.Request{{
					Procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
					Msg:       &taskv1.DeleteTasksRequest{TaskIds: []string{"task-writer"}, Records: true},
				}},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := paneltest.OpenAgents(t, buildReviewedTasks()...)
				harness.Press(tt.choose...)
				harness.Press("space", "a")
				before := harness.Requests()

				harness.Press(tt.press...)

				harness.AssertSent(append(before, tt.wantSent...)...)
			})
		}
	})

	t.Run("the workflow menu stays up until the archive lands", func(t *testing.T) {
		archive := workflowv1connect.WorkflowServiceArchiveWorkflowProcedure
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Hold(archive)

		harness.Press("space", "a", "a")

		frame := harness.Frame()
		assert.Contains(t, frame, "archive with its agents", "the menu is still rendered")
		assert.Contains(t, frame, "archiving api…")

		harness.Release(archive)

		require.Contains(t, harness.Frame(), "archive with its agents", "the agents are still on the list")

		harness.Watch(paneltest.Snapshot())

		assert.NotContains(t, harness.Frame(), "archive with its agents")
	})

	t.Run("a task delete keeps its form up until the watch drops the task", func(t *testing.T) {
		harness := openTaskForms(t, newFormAuthor())

		harness.Press("j", "space", "a", "down", "enter", "enter")

		require.Contains(t, harness.Frame(), `deleting task "task-a1"`)

		harness.Watch(paneltest.Snapshot(newFormWorkflow()))

		assert.NotContains(t, harness.Frame(), `deleting task "task-a1"`)
		assert.NotContains(t, harness.Frame(), "enter submit")
	})

	t.Run("a delete the watch never drops closes its form once the wait runs out", func(t *testing.T) {
		harness := openTaskForms(t, newFormAuthor())

		harness.Press("j", "space", "a", "down", "enter", "enter")
		require.Contains(t, harness.Frame(), `deleting task "task-a1"`)

		harness.Advance(5 * time.Second)

		assert.NotContains(t, harness.Frame(), `deleting task "task-a1"`)
	})

	t.Run("a refused archive takes the menu down", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Refuse(agentv1connect.AgentServiceArchiveAgentsProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("already archived")))

		harness.Press("j", "space", "a", "a")

		assert.NotContains(t, harness.Frame(), "archive and delete for good")
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
	})

	t.Run("a refused workflow archive says why and keeps the workflow", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Refuse(workflowv1connect.WorkflowServiceArchiveWorkflowProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("agents still running")))

		harness.Press(slices.Concat([]string{"space", "a", "enter"}, paneltest.WidenListPresses)...)

		assert.Contains(t, paneltest.ListRows(harness.Frame()), "api")
		assert.Contains(t, harness.ReadMessages(), "agents still running")
	})

	t.Run("a refused archive puts the agent's pane back on the stage", func(t *testing.T) {
		reviewer := newLiveAuthor()
		reviewer.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
		harness := openForms(t, newFormWorkflow(), reviewer)
		harness.Press("j")
		harness.Advance(time.Second)
		require.Equal(t, "%9", harness.Tmux().GuestPane())
		harness.Refuse(agentv1connect.AgentServiceArchiveAgentsProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("agent.pre-archive hook refused")))

		harness.Press("space", "a", "a")

		assert.Equal(t, "%9", harness.Tmux().GuestPane())
		assert.NotContains(t, harness.Frame(), "dormant")
	})

	t.Run("a refused task delete leaves the agent on it live", func(t *testing.T) {
		harness := openTaskForms(t, newLiveAuthor())
		harness.Press("j")
		harness.Advance(time.Second)
		harness.Refuse(taskv1connect.TaskServiceDeleteTasksProcedure, connect.NewError(connect.CodeInternal, errors.New("delete failed")))

		harness.Press("space", "a", "down", "enter", "enter")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.True(t, isListed(harness, "└─ task-a1"))
		assert.Equal(t, "%9", harness.Tmux().GuestPane())
	})

	t.Run("an archive in flight takes the pane off the stage and leaves the agent live until it lands", func(t *testing.T) {
		reviewer := newLiveAuthor()
		reviewer.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
		harness := openForms(t, newFormWorkflow(), reviewer)
		harness.Press("j")
		harness.Advance(time.Second)
		require.Equal(t, "%9", harness.Tmux().GuestPane())
		harness.Hold(agentv1connect.AgentServiceArchiveAgentsProcedure)

		harness.Press("space", "a", "a")

		assert.Empty(t, harness.Tmux().GuestPane())
		t.Log(paneltest.ListRows(harness.Frame()), harness.Tmux().GuestPane())
		assert.True(t, isListed(harness, "author"))

		harness.Press("esc")
		harness.Advance(time.Second)

		assert.Empty(t, harness.Tmux().GuestPane(), "walking away from the form does not stage the pane the write is killing")

		harness.Release(agentv1connect.AgentServiceArchiveAgentsProcedure)

		assert.True(t, isListed(harness, "author"), "the agent stays until the watch drops it")

		harness.Watch(paneltest.Deleted(watchv1.Resource_RESOURCE_AGENT, "a1"))

		assert.False(t, isListed(harness, "author"))
		assert.NotContains(t, paneltest.ListRows(harness.Frame()), "author")
	})

	t.Run("a watch that lands while an archive is in flight waits for the archive", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Hold(agentv1connect.AgentServiceArchiveAgentsProcedure)
		harness.Press("j", "space", "a", "a")
		held := harness.Frame()

		harness.Watch(paneltest.Deleted(watchv1.Resource_RESOURCE_AGENT, "a1"))

		assert.Equal(t, held, harness.Frame(), "the list and the menu stay as they were")

		harness.Release(agentv1connect.AgentServiceArchiveAgentsProcedure)

		frame := harness.Frame()
		assert.NotContains(t, paneltest.ListRows(frame), "author")
		assert.NotContains(t, frame, "archive this agent")
	})

	t.Run("a refused task delete says why", func(t *testing.T) {
		harness := openTaskForms(t, newFormAuthor())
		harness.Refuse(taskv1connect.TaskServiceDeleteTasksProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("already deleting")))

		harness.Press(slices.Concat([]string{"j", "space", "a", "down", "enter", "enter"}, paneltest.WidenListPresses)...)

		assert.Contains(t, harness.Frame(), `Could not delete task "task-a1"`)
	})
}

// TestFormsKeys covers the keys a form answers to.
func TestFormsKeys(t *testing.T) {
	t.Run("a workflow edited to a blank name sends nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press("space", "E", "ctrl+u", "space", "space", "enter")

		assert.Empty(t, harness.Sent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure))
	})

	t.Run("the adopt key on an agent row opens nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press("j", "a")

		assert.NotContains(t, harness.Frame(), "Adopt")
		assert.Equal(t, "└─ author", paneltest.HoveredRow(harness.Frame()))
	})
}

func openForms(t *testing.T, workflow *workflowv1.Workflow, author *agentv1.Agent) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, workflow, author)
	harness.Press("g", "g")

	return harness
}

func openTaskForms(t *testing.T, author *agentv1.Agent) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, newFormWorkflow(), author, paneltest.TaskOf(author))
	harness.Press("g", "g")

	return harness
}

const workflowTopFieldCount = 4

func openWorkflowSubformKeys(index int) []string {
	return slices.Concat(slices.Repeat([]string{"tab"}, workflowTopFieldCount), slices.Repeat([]string{"down"}, index), []string{"enter"})
}

func newFormWorkflow() *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:      "w1",
		Name:    "api",
		Harness: "claude",
		Cwd:     "/work/api",
		Flags:   "--model opus",
	}
}

func newConfiguredWorkflow() *workflowv1.Workflow {
	workflow := newFormWorkflow()
	workflow.Context = "shared notes"
	workflow.Worktree = &workflowv1.WorktreeConfig{Enabled: true}

	return workflow
}

func newFormAuthor() *agentv1.Agent {
	author := paneltest.Agent("a1", "author", "w1")
	author.Cwd = "/work/api"
	author.Harness = "claude"
	author.Flags = "--model opus"

	return author
}

func newLiveAuthor() *agentv1.Agent {
	author := paneltest.Agent("a1", "author", "w1")
	author.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	author.PaneId = "%9"
	author.Alive = xptr.Ptr(true)

	return author
}

var ignoringEventPrompts = []cmp.Option{protocmp.IgnoreFields(&workflowv1.CreateWorkflowRequest{}, "event_prompts")}

func isListed(harness *paneltest.Panel, name string) bool {
	return slices.ContainsFunc(paneltest.ListRows(harness.Frame()), func(row string) bool {
		return strings.Contains(row, name)
	})
}

func answerAgentCreate(harness *paneltest.Panel, pane string) {
	harness.Answer(agentv1connect.AgentServiceCreateAgentProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: &agentv1.Agent{Id: "a2", PaneId: pane}}), nil
	})
}

func newCreatedAgent() *agentv1.Agent {
	agent := paneltest.Agent("a2", "check-ci", "w1")
	agent.PaneId = "%9"
	agent.Alive = xptr.Ptr(true)

	return agent
}
