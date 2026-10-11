package ui_test

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/eventprompts"
	"komrad/internal/ui"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	hookv1 "komrad/proto/gen/hook/v1"
	"komrad/proto/gen/hook/v1/hookv1connect"
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

// TestFormsSubmit covers every form sending what was typed into it and closing once the write lands.
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
			press:     slices.Concat([]string{"space", "c"}, spellKeys("run the checks"), []string{"tab", "ctrl+u"}, spellKeys("codex"), []string{"enter"}),
			answer:    connect.NewResponse(&agentv1.CreateAgentResponse{Agent: &agentv1.Agent{Id: "a2"}}),
			settle:    []*watchv1.WatchEvent{buildPuts(newAgent("a2", "ci", "w1"))},
			procedure: agentv1connect.AgentServiceCreateAgentProcedure,
			want: &agentv1.CreateAgentRequest{
				Prompt:     "run the checks",
				Harness:    "codex",
				WorkflowId: "w1",
			},
		},
		{
			name: "the adopt form sends the ref and prompt into the workflow",
			press: slices.Concat([]string{"space", "A"}, spellKeys("https://github.com/acmecorp/admin/pull/1069"), []string{"tab"},
				spellKeys("pick it up"), []string{"enter"}),
			answer:    connect.NewResponse(&agentv1.AdoptResponse{Agent: &agentv1.Agent{Id: "a2"}}),
			settle:    []*watchv1.WatchEvent{buildPuts(newAgent("a2", "adopted", "w1"))},
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
			press:     slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"enter"}),
			answer:    connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}),
			settle:    []*watchv1.WatchEvent{buildPuts(newLiveWorkflow("w2", "web"))},
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			want: &workflowv1.CreateWorkflowRequest{
				Name:     "web",
				Cwd:      "/work/from-tmux",
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
		},
		{
			name: "the new-workflow form with no provisioner makes no worktrees",
			press: slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"tab"}, spellKeys("the frontend"), []string{"tab"},
				spellKeys("/work/web"), []string{"tab", "up", "tab", "enter", "tab"}, spellKeys("--model sonnet"), []string{"tab"}, spellKeys("codex"),
				[]string{"tab"}, spellKeys("--model gpt-5"), []string{"enter"}),
			answer:    connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}),
			settle:    []*watchv1.WatchEvent{buildPuts(newLiveWorkflow("w2", "web"))},
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
			name: "the new-workflow form with worktrees sends each path list without its blank lines",
			press: slices.Concat([]string{"space", "w"}, spellKeys("web"), openWorkflowSectionKeys(1),
				spellKeys("develop"), []string{"tab", "tab"}, spellKeys(".env"), []string{"ctrl+j", "ctrl+j"}, spellKeys("config/local.json"),
				[]string{"tab"}, spellKeys("node_modules"), []string{"enter"}),
			answer:    connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "w2"}}),
			settle:    []*watchv1.WatchEvent{buildPuts(newLiveWorkflow("w2", "web"))},
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			want: &workflowv1.CreateWorkflowRequest{
				Name: "web",
				Worktree: &workflowv1.WorktreeConfig{
					Enabled:      true,
					BaseBranch:   "develop",
					CopyPaths:    []string{".env", "config/local.json"},
					SymlinkPaths: []string{"node_modules"},
				},
			},
		},
		{
			name:      "the workflow edit form sends the field edited and the ones that were not",
			workflow:  newConfiguredWorkflow(),
			press:     slices.Concat([]string{"space", "E", "tab", "tab", "ctrl+u"}, spellKeys("/work/api-v2"), []string{"enter"}),
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
				buildDeleted(watchv1.Resource_RESOURCE_WORKFLOW, "w1"),
				buildDeleted(watchv1.Resource_RESOURCE_AGENT, "a1"),
			},
			procedure: workflowv1connect.WorkflowServiceArchiveWorkflowProcedure,
			want:      &workflowv1.ArchiveWorkflowRequest{Id: "w1"},
		},
		{
			name:   "the workflow archive prompt archives the workflow from its orchestrator's menu",
			author: newOrchestrator(),
			press:  []string{"space", "a", "enter"},
			settle: []*watchv1.WatchEvent{
				buildDeleted(watchv1.Resource_RESOURCE_WORKFLOW, "w1"),
				buildDeleted(watchv1.Resource_RESOURCE_AGENT, "o1"),
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
			var harness *panelHarness
			if tt.empty {
				harness = openAgents(t)
			} else {
				harness = openForms(t, workflow, author)
			}
			harness.panel.SetHostPath(tt.hostPath)
			if tt.answer != nil {
				harness.answer(tt.procedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
					return tt.answer, nil
				})
			}

			harness.press(tt.press...)
			if len(tt.settle) > 0 {
				harness.watch(tt.settle...)
			}

			harness.assertSentToWith(tt.procedure, ignoringEventPrompts, tt.want)
			assert.NotContains(t, harness.readFrame(), "Could not", "the server accepted the write")
			assert.NotContains(t, harness.readFrame(), "enter submit", "and the form it closed is gone")
		})
	}
}

// TestFormsNewAgentWorktree covers the one thing an agent can say for itself about its worktree.
func TestFormsNewAgentWorktree(t *testing.T) {
	workflow := newFormWorkflow()
	workflow.Worktree = &workflowv1.WorktreeConfig{Enabled: true}

	t.Run("the field opens on what the workflow does", func(t *testing.T) {
		harness := openForms(t, workflow, newFormAuthor())

		harness.press("space", "c")

		assert.Contains(t, harness.readFrame(), "› Same as workflow")
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

			harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go"), tt.press, []string{"enter"})...)

			sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
			require.Len(t, sent, 1)
			create, ok := sent[0].(*agentv1.CreateAgentRequest)
			require.True(t, ok)
			assert.Equal(t, tt.want, create.ProvisionEnabled)
		})
	}
}

// TestFormsRefused covers a refused write leaving the form up, holding what was typed.
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
			press:     slices.Concat([]string{"space", "c"}, spellKeys("check CI"), []string{"enter"}),
			typed:     "check CI",
		},
		{
			name:      "the new-workflow form",
			procedure: workflowv1connect.WorkflowServiceCreateWorkflowProcedure,
			press:     slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"enter"}),
			typed:     "web",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openForms(t, newFormWorkflow(), newFormAuthor())
			harness.refuse(tt.procedure, connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed")))

			harness.press(tt.press...)

			assert.Contains(t, harness.readFrame(), tt.typed, "the form is still up holding what was filled in")
			assert.Contains(t, harness.readFrame(), "not allowed", "the reason is rendered under the form")

			harness.press("ctrl+g")
			frame := harness.readFrame()
			assert.Contains(t, frame, "not allowed", "a refused write is reported")
			assert.Contains(t, frame, "fix and submit again", "ctrl+g opens the whole refusal over the form")

			harness.press("ctrl+g", "!")
			assert.Contains(t, harness.readFrame(), tt.typed+"!", "and the form takes keys again")
		})
	}
}

// TestFormsAdoptExisting covers an adoption answered with the agent already on the branch.
func TestFormsAdoptExisting(t *testing.T) {
	tucked := newAgent("a4", "tucked", "w1")
	tuckedTask := newTaskOf(tucked)
	tuckedTask.Hidden = true
	nested := newChild("a5", "nested", "a4")
	nested.TaskId = tucked.TaskId

	tests := []struct {
		name        string
		existing    *agentv1.Agent
		wantScreen  string
		wantHovered string
	}{
		{
			name:        "a live agent lands the agents list on its row",
			existing:    newFormAuthor(),
			wantScreen:  agentsScreen,
			wantHovered: "author",
		},
		{
			name:        "an agent on a hidden task shows the hidden tasks and lands on its row",
			existing:    tucked,
			wantScreen:  agentsScreen,
			wantHovered: "tucked",
		},
		{
			name:        "a child on a hidden task shows the hidden tasks and lands on its row",
			existing:    nested,
			wantScreen:  agentsScreen,
			wantHovered: "nested",
		},
		{
			name:        "an archived agent lands the archive on its row",
			existing:    newArchivedAgent("a3", "old-work", "w1"),
			wantScreen:  agentsScreen,
			wantHovered: "old-work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, newFormWorkflow(), newFormAuthor(), newArchivedAgent("a3", "old-work", "w1"), tucked, tuckedTask, nested)
			harness.press("g", "g")
			harness.answer(agentv1connect.AgentServiceAdoptProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
				return connect.NewResponse(&agentv1.AdoptResponse{Agent: tt.existing, Existing: true}), nil
			})

			harness.press(slices.Concat([]string{"space", "A"}, spellKeys("someones/work"), []string{"enter"})...)

			frame := harness.readFrame()
			assert.Equal(t, tt.wantScreen, harness.readScreen())
			assert.Contains(t, readHoveredRow(frame), tt.wantHovered)
			assert.Contains(t, frame, "This agent already exists")
			assert.NotContains(t, frame, "Could not adopt")
		})
	}
}

// TestFormsInFlight covers the new-agent form while its create has not come back.
func TestFormsInFlight(t *testing.T) {
	create := agentv1connect.AgentServiceCreateAgentProcedure
	submit := slices.Concat([]string{"space", "c"}, spellKeys("check CI"), []string{"enter"})

	t.Run("the form stays up saying it is submitting until the create lands", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		answerAgentCreate(harness, "")
		harness.hold(create)

		harness.press(submit...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "starting the agent…")
		assert.Contains(t, frame, "check CI", "the form is still up")
		assert.Empty(t, harness.panel.ReadRespawns(), "no pane is opened for the provisioner")

		harness.release(create)

		assert.Contains(t, harness.readFrame(), "check CI", "the form waits for the row the create made")

		harness.watch(buildPuts(newAgent("a2", "check-ci", "w1")))

		frame = harness.readFrame()
		assert.NotContains(t, frame, "starting the agent…")
		assert.NotContains(t, frame, "check CI", "the form goes once the row is shown")
	})

	t.Run("the form keeps saying so while the pane the create made warms up", func(t *testing.T) {
		frames := &harnessFrameFake{}
		harness := newPanelHarness(t, panelHarnessParams{harnessFrames: frames})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow(), newFormAuthor()))
		answerAgentCreate(harness, "%9")
		harness.press("g", "g")

		harness.press(submit...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "starting the agent…")
		assert.Contains(t, frame, "check CI", "the form is still up")

		harness.watch(buildPuts(newCreatedAgent()))
		assert.Contains(t, harness.readFrame(), "check CI", "the row alone does not close it")

		frames.isShowing = true
		harness.fireTimers()

		frame = harness.readFrame()
		assert.NotContains(t, frame, "starting the agent…")
		assert.NotContains(t, frame, "check CI", "the form goes with the reveal")
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("a row that lands before its create comes back takes the cursor with the create", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		answerAgentCreate(harness, "")
		harness.hold(create)
		harness.press(submit...)

		harness.watch(buildPuts(newAgent("a2", "check-ci", "w1")))
		harness.release(create)

		frame := harness.readFrame()
		assert.NotContains(t, frame, "check CI", "the form goes once the create lands")
		assert.Contains(t, readHoveredRow(frame), "check-ci")
		assert.Contains(t, readTrail(frame), "check-ci")
	})

	t.Run("a second submit sends nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.hold(create)
		harness.press(submit...)

		harness.press("enter", "!")

		assert.Len(t, harness.readSent(create), 1, "the key never reached the submit path")
		assert.Contains(t, harness.readFrame(), "check CI", "and the form is as it was")
		assert.NotContains(t, harness.readFrame(), "check CI!")
	})

	t.Run("esc hands the panel back and the create still lands without taking the cursor", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		answerAgentCreate(harness, "")
		harness.hold(create)
		harness.press(submit...)

		harness.press("esc")

		assert.NotContains(t, harness.readFrame(), "what the agent is told", "the form it was holding is gone")
		assert.Contains(t, readLegend(harness.readFrame()), "⠋ starting", "the create it left is rendered in the corner")
		harness.press("j")
		assert.Equal(t, "└─ author", readHoveredRow(harness.readFrame()), "the panel takes keys again")

		harness.release(create)
		harness.watch(buildPuts(newAgent("a2", "check-ci", "w1")))

		frame := harness.readFrame()
		assert.Equal(t, "\uf444 local", readEngineLine(frame), "nothing is reported")
		assert.Equal(t, "└─ author", readHoveredRow(frame), "the cursor stays where the reader left it")
	})

	t.Run("an escaped create landing leaves a later form held", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.hold(create)
		harness.press(submit...)
		harness.press("esc")
		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("the second one"), []string{"enter"})...)

		harness.release(create)

		frame := harness.readFrame()
		assert.Contains(t, frame, "starting the agent…", "the second form is still held")
		assert.Contains(t, frame, "the second one")
	})
}

// TestFormsArchivePrompt covers the ways the agent and task archive menus can be answered.
func TestFormsArchivePrompt(t *testing.T) {
	t.Run("an agent's menu archives that one agent and nothing more", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press("j", "space", "a")

		frame := harness.readFrame()
		assert.Contains(t, frame, "› archive this agent")
		assert.Contains(t, frame, "back")
		assert.NotContains(t, frame, "for good")
	})

	t.Run("a task's menu opens on the reversible answer", func(t *testing.T) {
		harness := openTaskForms(t, newFormAuthor())

		harness.press("j", "space", "a")

		frame := harness.readFrame()
		assert.Contains(t, frame, "› archive with its agents")
		assert.Contains(t, frame, "archive and delete for good")
		assert.Contains(t, frame, "back")
	})

	t.Run("an agent's archive menu", func(t *testing.T) {
		tests := []struct {
			name     string
			press    []string
			wantSent []request
		}{
			{
				name:  "submitting without moving archives the agent",
				press: []string{"enter"},
				wantSent: []request{{
					procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
					msg:       &agentv1.ArchiveAgentsRequest{Ids: []string{"a1"}},
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
				harness.press("j", "space", "a")
				before := harness.readRequests()

				harness.press(tt.press...)

				harness.assertSent(append(before, tt.wantSent...)...)
				assert.Equal(t, agentsScreen, harness.readScreen(), "every answer lands back on the list")
			})
		}
	})

	t.Run("a task's archive menu", func(t *testing.T) {
		tests := []struct {
			name     string
			press    []string
			wantSent []request
		}{
			{
				name:  "submitting without moving archives the task and keeps the record",
				press: []string{"enter"},
				wantSent: []request{{
					procedure: taskv1connect.TaskServiceArchiveTaskProcedure,
					msg:       &taskv1.ArchiveTaskRequest{Task: "task-a1"},
				}},
			},
			{
				name:  "one row down deletes the task once confirmed",
				press: []string{"down", "enter", "y"},
				wantSent: []request{{
					procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
					msg:       &taskv1.DeleteTasksRequest{Tasks: []string{"task-a1"}},
				}},
			},
			{
				name:  "declining the confirm deletes nothing",
				press: []string{"down", "enter", "n"},
			},
			{
				name:  "the last row leaves the task where it is",
				press: []string{"down", "down", "enter"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openTaskForms(t, newFormAuthor())
				harness.press("j", "space", "a")
				before := harness.readRequests()

				harness.press(tt.press...)

				harness.assertSent(append(before, tt.wantSent...)...)
				assert.Equal(t, agentsScreen, harness.readScreen(), "every answer lands back on the list")
			})
		}
	})

	t.Run("a selection's archive menu", func(t *testing.T) {
		siblings := []string{"j", "j", "j", "v", "j", "v"}
		wholeTask := []string{"j", "v"}

		tests := []struct {
			name     string
			choose   []string
			press    []string
			wantSent []request
		}{
			{
				name:   "archiving chosen agents leaves the rest of their task standing",
				choose: siblings,
				press:  []string{"enter"},
				wantSent: []request{{
					procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
					msg:       &agentv1.ArchiveAgentsRequest{Ids: []string{"a4", "a3"}},
				}},
			},
			{
				name:   "deleting chosen agents leaves the rest of their task standing",
				choose: siblings,
				press:  []string{"down", "enter", "y"},
				wantSent: []request{
					{
						procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
						msg:       &agentv1.ArchiveAgentsRequest{Ids: []string{"a4", "a3"}},
					},
					{
						procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
						msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"a4", "a3"}},
					},
				},
			},
			{
				name:   "archiving every agent on a task archives the task",
				choose: wholeTask,
				press:  []string{"enter"},
				wantSent: []request{{
					procedure: taskv1connect.TaskServiceArchiveTaskProcedure,
					msg:       &taskv1.ArchiveTaskRequest{Task: "task-author"},
				}},
			},
			{
				name:   "deleting every agent on a task deletes the task",
				choose: wholeTask,
				press:  []string{"down", "enter", "y"},
				wantSent: []request{{
					procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
					msg:       &taskv1.DeleteTasksRequest{Tasks: []string{"task-author"}},
				}},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := openAgents(t, buildTaskFamily()...)
				harness.press(tt.choose...)
				harness.press("space", "a")
				before := harness.readRequests()

				harness.press(tt.press...)

				harness.assertSent(append(before, tt.wantSent...)...)
			})
		}

		t.Run("an agent joining a chosen task before the confirm keeps the task standing", func(t *testing.T) {
			harness := openAgents(t, buildTaskFamily()...)
			harness.press(wholeTask...)
			harness.press("space", "a", "down", "enter")
			before := harness.readRequests()

			late := newAgentAt("late", "w1", 3)
			late.TaskId = xptr.Ptr("task-author")
			harness.watch(buildSnapshot(append(buildTaskFamily(), late)...))
			harness.press("y")

			ids := []string{"author", "a4", "a3"}
			harness.assertSent(append(before,
				request{
					procedure: agentv1connect.AgentServiceArchiveAgentsProcedure,
					msg:       &agentv1.ArchiveAgentsRequest{Ids: ids},
				},
				request{
					procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
					msg:       &agentv1.DeleteAgentsRequest{Ids: ids},
				},
			)...)
		})
	})

	t.Run("the workflow menu stays up until the archive lands", func(t *testing.T) {
		archive := workflowv1connect.WorkflowServiceArchiveWorkflowProcedure
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.hold(archive)

		harness.press("space", "a", "a")

		frame := harness.readFrame()
		assert.Contains(t, frame, "archive with its agents", "the menu is still rendered")
		assert.Contains(t, frame, "archiving api…")

		harness.release(archive)

		require.Contains(t, harness.readFrame(), "archive with its agents", "the agents are still on the list")

		harness.watch(buildSnapshot())

		assert.NotContains(t, harness.readFrame(), "archive with its agents")
	})

	t.Run("a task delete keeps its form up until the watch drops the task", func(t *testing.T) {
		harness := openTaskForms(t, newFormAuthor())

		harness.press("j", "space", "a", "down", "enter", "y")

		require.Contains(t, harness.readFrame(), `deleting task "task-a1"`)

		harness.watch(buildSnapshot(newFormWorkflow()))

		assert.NotContains(t, harness.readFrame(), `deleting task "task-a1"`)
		assert.NotContains(t, harness.readFrame(), "enter submit")
	})

	t.Run("a delete the watch never drops closes its form once the wait runs out", func(t *testing.T) {
		harness := openTaskForms(t, newFormAuthor())

		harness.press("j", "space", "a", "down", "enter", "y")
		require.Contains(t, harness.readFrame(), `deleting task "task-a1"`)

		harness.panel.ExpireParked()
		harness.fireMatching(ui.IsWarmupTick)

		assert.NotContains(t, harness.readFrame(), `deleting task "task-a1"`)
	})

	t.Run("a refused archive takes the menu down", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.refuse(agentv1connect.AgentServiceArchiveAgentsProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("already archived")))

		harness.press("j", "space", "a", "a")

		assert.NotContains(t, harness.readFrame(), "archive and delete for good")
		assert.Equal(t, agentsScreen, harness.readScreen())
	})

	t.Run("a confirmed archive still in flight keeps a rebuilt panel up", func(t *testing.T) {
		binary := &fakeBinary{}
		harness := newPanelHarness(t, panelHarnessParams{binary: binary})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow(), newFormAuthor()))
		harness.hold(agentv1connect.AgentServiceArchiveAgentsProcedure)
		harness.press("g", "g", "j", "space", "a", "enter")

		binary.isReplaced = true
		harness.fireTimers()
		require.NotContains(t, harness.readFrame(), "komrad was rebuilt", "the write is still in flight")

		harness.release(agentv1connect.AgentServiceArchiveAgentsProcedure)
		harness.watch(buildSnapshot(newFormWorkflow()))
		harness.fireTimers()
		assert.Contains(t, harness.readFrame(), "komrad was rebuilt")
	})

	t.Run("a refused workflow archive says why and keeps the workflow", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.refuse(workflowv1connect.WorkflowServiceArchiveWorkflowProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("agents still running")))

		harness.press(slices.Concat([]string{"space", "a", "enter"}, widenListPresses)...)

		assert.Contains(t, readListRows(harness.readFrame()), "api")
		assert.Contains(t, readOpenMessages(harness), "agents still running")
	})

	t.Run("a refused archive puts the agent's pane back on the stage", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newLiveAuthor())
		harness.press("j")
		harness.fireTimers()
		require.Equal(t, "%9", harness.panel.ReadGuestPane())
		harness.refuse(agentv1connect.AgentServiceArchiveAgentsProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("agent.pre-archive hook refused")))

		harness.press("space", "a", "a")

		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.NotContains(t, harness.readFrame(), "dormant")
	})

	t.Run("a refused task delete leaves the agent on it live", func(t *testing.T) {
		harness := openTaskForms(t, newLiveAuthor())
		harness.press("j")
		harness.fireTimers()
		harness.refuse(taskv1connect.TaskServiceDeleteTasksProcedure, connect.NewError(connect.CodeInternal, errors.New("delete failed")))

		harness.press("space", "a", "down", "enter", "y")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.True(t, harness.panel.IsAgentAlive("a1"))
	})

	t.Run("an archive in flight takes the pane off the stage and leaves the agent live until it lands", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newLiveAuthor())
		harness.press("j")
		harness.fireTimers()
		require.Equal(t, "%9", harness.panel.ReadGuestPane())
		harness.hold(agentv1connect.AgentServiceArchiveAgentsProcedure)

		harness.press("space", "a", "a")

		assert.Empty(t, harness.panel.ReadGuestPane())
		assert.True(t, harness.panel.IsAgentAlive("a1"))

		harness.press("esc")
		harness.fireTimers()

		assert.Empty(t, harness.panel.ReadGuestPane(), "walking away from the form does not stage the pane the write is killing")

		harness.release(agentv1connect.AgentServiceArchiveAgentsProcedure)

		assert.True(t, harness.panel.IsAgentAlive("a1"), "the agent stays until the watch drops it")

		harness.watch(buildDeleted(watchv1.Resource_RESOURCE_AGENT, "a1"))

		assert.False(t, harness.panel.IsAgentAlive("a1"))
		assert.NotContains(t, readListRows(harness.readFrame()), "author")
	})

	t.Run("a watch that lands while an archive is in flight waits for the archive", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.hold(agentv1connect.AgentServiceArchiveAgentsProcedure)
		harness.press("j", "space", "a", "a")
		held := harness.readFrame()

		harness.watch(buildDeleted(watchv1.Resource_RESOURCE_AGENT, "a1"))

		assert.Equal(t, held, harness.readFrame(), "the list and the menu stay as they were")

		harness.release(agentv1connect.AgentServiceArchiveAgentsProcedure)

		frame := harness.readFrame()
		assert.NotContains(t, readListRows(frame), "author")
		assert.NotContains(t, frame, "archive this agent")
	})

	t.Run("a refused task delete says why", func(t *testing.T) {
		harness := openTaskForms(t, newFormAuthor())
		harness.refuse(taskv1connect.TaskServiceDeleteTasksProcedure, connect.NewError(connect.CodeFailedPrecondition, errors.New("already deleting")))

		harness.press(slices.Concat([]string{"j", "space", "a", "down", "enter", "y"}, widenListPresses)...)

		assert.Contains(t, harness.readFrame(), `Could not delete task "task-a1"`)
	})
}

// TestFormsKeys covers the keys a form answers to.
func TestFormsKeys(t *testing.T) {
	t.Run("enter submits from a text field", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go"), []string{"enter"})...)

		sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "go", create.GetPrompt())
	})

	t.Run("ctrl+j and alt+enter break a line in a text field", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("check CI"), []string{"ctrl+j"}, spellKeys("then"),
			[]string{"alt+enter"}, spellKeys("fix it"), []string{"enter"})...)

		sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "check CI\nthen\nfix it", create.GetPrompt())
	})

	t.Run("a workflow edited to a blank name sends nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press("space", "E", "ctrl+u", "space", "space", "enter")

		assert.Empty(t, harness.readSent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure))
	})

	t.Run("the adopt key on an agent row opens nothing", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press("j", "a")

		assert.NotContains(t, harness.readFrame(), "Adopt")
		assert.Equal(t, "└─ author", readHoveredRow(harness.readFrame()))
	})
}

// TestFormsWorkflowProvisioner covers the workflow form asking only what its provisioner needs.
func TestFormsWorkflowProvisioner(t *testing.T) {
	harness := openForms(t, newFormWorkflow(), newFormAuthor())

	harness.press(slices.Concat([]string{"space", "w"}, slices.Repeat([]string{"tab"}, 3))...)
	frame := harness.readFrame()
	assert.Contains(t, frame, "None: agents keep the directory they are given", "neither option is hidden by the other being picked")
	assert.Less(t, strings.Index(frame, "None: agents keep"), strings.Index(frame, "Worktree: a git worktree per agent"), "and the options keep their order")

	harness.press(slices.Concat([]string{"tab", "down", "enter", "tab", "tab"}, spellKeys(".env"), []string{"esc", "shift+tab", "up", "tab", "enter"})...)
	assert.NotContains(t, harness.readFrame(), "Worktree base branch", "none provisions nothing, so its worktree section does not open")

	harness.press("shift+tab", "down", "enter")

	sent := harness.readSent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)
	require.Len(t, sent, 1)
	create, ok := sent[0].(*workflowv1.CreateWorkflowRequest)
	require.True(t, ok)
	assert.Equal(t, []string{".env"}, create.GetWorktree().GetCopyPaths(), "switching back hands back what was typed before")
}

// TestFormsWorkflowSections covers drilling into a section of the workflow form and coming back.
func TestFormsWorkflowSections(t *testing.T) {
	t.Run("a section opens beside the top form rendered whole", func(t *testing.T) {
		harness := openWideForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), openWorkflowSectionKeys(0))...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "Default Agent")
		assert.Contains(t, frame, "where agents start, and the checkout worktrees are cut", "the top form keeps its width")
		assert.Contains(t, frame, "› agents ›", "with the section that is open marked")
	})

	t.Run("a long value folds at the form's widest", func(t *testing.T) {
		longContext := "Nobody archives anybody; the rows retire when the pull request merges or closes."
		workflow := newConfiguredWorkflow()
		workflow.Context = longContext
		harness := openWideForms(t, workflow, newFormAuthor())

		harness.press(append([]string{"space", "E"}, openWorkflowSectionKeys(1)...)...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "Worktree base branch", "the section still opens beside the top form")
		assert.NotContains(t, frame, longContext, "the context folds rather than running on one line")
	})

	t.Run("back keeps what the section was given and lands on the list of sections", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), openWorkflowSectionKeys(0), spellKeys("codex"), []string{"esc"})...)
		assert.NotContains(t, harness.readFrame(), "Default Agent", "the section closed")

		harness.press("enter")
		assert.Contains(t, harness.readFrame(), "Default Agent", "enter opens the section the cursor came back to")

		harness.press("enter")

		sent := harness.readSent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*workflowv1.CreateWorkflowRequest)
		require.True(t, ok)
		assert.Equal(t, "codex", create.GetHarness())
	})

	t.Run("back on the top form throws the edit away", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"esc"})...)

		assert.NotContains(t, harness.readFrame(), "New workflow name")
		assert.Empty(t, harness.readSent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure))
	})
}

// TestFormsWorkflowHooks covers the hook pickers offering what the hooks directory holds.
func TestFormsWorkflowHooks(t *testing.T) {
	every := []*workflowv1.WorkflowHook{
		{Event: config.HookAgentPreLaunch, Script: "install-deps"},
		{Event: config.HookAgentPostLaunch, Script: "announce"},
		{Event: config.HookAgentPreResume, Script: "warm-up"},
		{Event: config.HookAgentPostResume, Script: "announce-back"},
		{Event: config.HookAgentPreArchive, Script: "check-in"},
		{Event: config.HookAgentPostArchive, Script: "give-back-the-slot"},
		{Event: config.HookAgentPreRestore, Script: "lock"},
		{Event: config.HookAgentPostRestore, Script: "unlock"},
		{Event: config.HookAgentPreDelete, Script: "take-the-stack-down"},
		{Event: config.HookAgentPostDelete, Script: "say-goodbye"},
	}

	t.Run("a picker offers every hook and opens on the one the workflow names", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{{Event: config.HookAgentPreLaunch, Script: "copy-env"}}, "install-deps", "copy-env")

		frame := harness.readFrame()
		assert.Contains(t, frame, "install-deps")
		assert.Contains(t, frame, "› copy-env")
	})

	t.Run("a hook still waiting on a chmod is offered", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.answer(hookv1connect.HookServiceListHooksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&hookv1.ListHooksResponse{Hooks: []*hookv1.Hook{{Name: "unrunnable"}}}), nil
		})

		harness.press(append([]string{"space", "E"}, openWorkflowSectionKeys(2)...)...)

		assert.Contains(t, harness.readFrame(), "unrunnable")
	})

	t.Run("a hook the directory no longer holds is still offered", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{{Event: config.HookAgentPreLaunch, Script: "copy-env"}}, "install-deps")

		assert.Contains(t, harness.readFrame(), "missing")
	})

	t.Run("an empty hooks directory says where hooks go", func(t *testing.T) {
		harness := openHookForm(t, nil)

		assert.Contains(t, harness.readFrame(), "none found in your hooks directory")
	})

	t.Run("every moment comes back as it went in", func(t *testing.T) {
		harness := openHookForm(t, every, "install-deps", "announce", "warm-up", "announce-back", "check-in",
			"give-back-the-slot", "lock", "unlock", "take-the-stack-down", "say-goodbye")

		harness.press("enter")

		assertSentHooks(t, harness, every)
	})

	t.Run("a moment holding two keeps the one that ran first", func(t *testing.T) {
		harness := openHookForm(t, []*workflowv1.WorkflowHook{
			{Event: config.HookAgentPreLaunch, Script: "install-deps"},
			{Event: config.HookAgentPreLaunch, Script: "copy-env"},
		}, "install-deps", "copy-env")

		harness.press("enter")

		assertSentHooks(t, harness, []*workflowv1.WorkflowHook{{Event: config.HookAgentPreLaunch, Script: "install-deps"}})
	})
}

// TestFormsWorkflowEventPrompts pins the workflow form carrying a prompt for each event.
func TestFormsWorkflowEventPrompts(t *testing.T) {
	sentEventPrompts := func(t *testing.T, harness *panelHarness) []*workflowv1.WorkflowEventPrompt {
		t.Helper()

		sent := harness.readSent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure)
		require.Len(t, sent, 1)
		update, ok := sent[0].(*workflowv1.UpdateWorkflowRequest)
		require.True(t, ok)

		return update.GetEventPrompts()
	}

	t.Run("the prompts come back in the order the events are listed", func(t *testing.T) {
		workflow := newFormWorkflow()
		workflow.EventPrompts = []*workflowv1.WorkflowEventPrompt{
			{Event: "review.approved", Prompt: "open the pull request"},
			{Event: "pull_request.checks_failing", Prompt: "fix the checks"},
		}
		harness := openForms(t, workflow, newFormAuthor())

		harness.press("space", "E", "enter")

		assert.Empty(t, cmp.Diff([]*workflowv1.WorkflowEventPrompt{
			{Event: "pull_request.checks_failing", Prompt: "fix the checks"},
			{Event: "review.approved", Prompt: "open the pull request"},
		}, sentEventPrompts(t, harness), protocmp.Transform()))
	})

	t.Run("a prompt typed under its event is sent with it", func(t *testing.T) {
		harness := openWideForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(append([]string{"space", "E"}, openWorkflowSectionKeys(3)...)...)
		frame := harness.readFrame()
		assert.Contains(t, frame, "when some checks on the head failed while others still run")

		harness.press(slices.Concat(spellKeys("fix the checks"), []string{"enter"})...)

		assert.Empty(t, cmp.Diff([]*workflowv1.WorkflowEventPrompt{
			{Event: "pull_request.checks_failing_running", Prompt: "fix the checks"},
		}, sentEventPrompts(t, harness), protocmp.Transform()))
	})

	t.Run("a muted event with no prompt comes back muted", func(t *testing.T) {
		workflow := newFormWorkflow()
		workflow.EventPrompts = []*workflowv1.WorkflowEventPrompt{
			{Event: "pull_request.checks_failing", Prompt: "fix the checks"},
			{Event: "pull_request.behind", Muted: true},
		}
		harness := openForms(t, workflow, newFormAuthor())

		harness.press("space", "E", "enter")

		assert.Empty(t, cmp.Diff(workflow.EventPrompts, sentEventPrompts(t, harness), protocmp.Transform()))
	})

	t.Run("muting an event under its prompt sends it muted", func(t *testing.T) {
		harness := openWideForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(append([]string{"space", "E"}, openWorkflowSectionKeys(3)...)...)
		harness.press(slices.Concat(spellKeys("fix the checks"), []string{"tab"})...)
		frame := harness.readFrame()
		assert.Contains(t, frame, "only rewrites mail")

		harness.press("down", "enter")

		assert.Empty(t, cmp.Diff([]*workflowv1.WorkflowEventPrompt{
			{
				Event:  "pull_request.checks_failing_running",
				Prompt: "fix the checks",
				Muted:  true,
			},
		}, sentEventPrompts(t, harness), protocmp.Transform()))
	})

	t.Run("a new workflow starts with each event's default", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"enter"})...)

		sent := harness.readSent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*workflowv1.CreateWorkflowRequest)
		require.True(t, ok)
		require.Len(t, create.GetEventPrompts().GetPrompts(), len(eventprompts.All()))
		assert.Contains(t, create.GetEventPrompts().GetPrompts()[0].GetPrompt(), "Read the failed jobs' logs")
	})
}

// TestFormsFieldEditor covers the editor key opening a form's text field in the reader's editor.
func TestFormsFieldEditor(t *testing.T) {
	t.Run("the field under the cursor is written out for the editor", func(t *testing.T) {
		harness := openFieldEditor(t)

		harness.press("space", "E", "tab", "ctrl+e")

		draft, err := os.ReadFile(ui.FormFieldDraftPath("context"))
		require.NoError(t, err)
		assert.Equal(t, "shared notes", string(draft))
	})

	t.Run("a form with no text field of its own leaves the key to the form", func(t *testing.T) {
		harness := openFieldEditor(t)

		harness.press("r", "ctrl+e")

		_, err := os.Stat(ui.FormFieldDraftPath("name"))
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	tests := []struct {
		name  string
		press []string
		field string
		want  *workflowv1.CreateWorkflowRequest
	}{
		{
			name:  "the context of a workflow that provisions nothing",
			press: slices.Concat([]string{"space", "w"}, spellKeys("web"), slices.Repeat([]string{"tab"}, 3), []string{"up", "shift+tab", "shift+tab"}),
			field: "context",
			want: &workflowv1.CreateWorkflowRequest{
				Name:     "web",
				Context:  "what the editor wrote!",
				Worktree: &workflowv1.WorktreeConfig{},
			},
		},
		{
			name:  "a list field of a workflow that makes worktrees",
			press: slices.Concat([]string{"space", "w"}, spellKeys("web"), openWorkflowSectionKeys(1), []string{"tab", "tab"}),
			field: "worktree_copy_paths",
			want: &workflowv1.CreateWorkflowRequest{
				Name: "web",
				Worktree: &workflowv1.WorktreeConfig{
					Enabled:   true,
					CopyPaths: []string{"what the editor wrote!"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFieldEditor(t)
			harness.press(append(tt.press, "ctrl+e")...)

			harness.send(harness.panel.NewFieldEdited(tt.field, "what the editor wrote\n"))
			harness.press("!", "enter")

			harness.assertSentToWith(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, ignoringEventPrompts, tt.want)
		})
	}
}

// TestFormsLayout covers a form being rendered in a column beside the list it was opened over.
func TestFormsLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		press []string
		want  string
	}{
		{
			name:  "a form takes the stage beside the list",
			press: []string{"space", "c"},
			want:  `Child of "author"`,
		},
		{
			name:  "a confirmation takes the stage beside the list",
			press: []string{"space", "a", "d"},
			want:  `Permanently delete task "task-a1"?`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openTaskForms(t, newLiveAuthor())
			harness.press("j")
			harness.fireTimers()

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Contains(t, readStage(frame)[0], tt.want)
			assert.Contains(t, readListRows(frame), "   └─ author", "the list it was opened over is still beside it")
		})
	}

	t.Run("closing the form hands the window back", func(t *testing.T) {
		t.Parallel()

		harness := openForms(t, newFormWorkflow(), newLiveAuthor())
		harness.press("j")
		harness.fireTimers()

		harness.press("r", "esc")

		assert.False(t, harness.panel.IsZoomed())
	})

	t.Run("a held form renders its notice under it at every height", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name   string
			height int
		}{
			{
				name:   "a short window",
				height: 24,
			},
			{
				name:   "a middling window",
				height: 32,
			},
			{
				name:   "a tall window",
				height: 40,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness := newPanelHarness(t, panelHarnessParams{
					width:  160,
					height: tt.height,
				})
				answerWorktreeReads(harness)
				harness.watch(buildSnapshot(newFormWorkflow()))
				harness.hold(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)

				harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"enter"})...)

				lines := splitLines(strings.TrimSuffix(harness.readFrame(), "\n"))
				assert.Len(t, lines, tt.height)
				assert.NotContains(t, lines[0], "creating workflow", "the panel keeps its top row")
				notice := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "creating workflow web…") })
				form := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "event prompts ›") })
				assert.Greater(t, notice, form, "the notice sits under the form")
			})
		}
	})

	t.Run("a window too narrow for both halves renders the form whole", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 60})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow(), newAgent("a1", "author", "w1")))

		harness.press("k", "space", "w")

		frame := harness.readFrame()
		assert.Contains(t, splitLines(frame)[0], "New workflow name")
		assert.NotContains(t, frame, "author", "the list gives the form its room")
	})

	t.Run("a refusal is read in full from inside the form", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 160})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newFormWorkflow()))
		harness.refuse(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no such workflow")))
		harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), []string{"enter"})...)

		harness.press("ctrl+g")
		frame := harness.readFrame()
		assert.Contains(t, frame, "Could not create the workflow")
		assert.NotContains(t, frame, "New workflow name", "the form gives up what is rendered in its column")

		harness.press("ctrl+g")
		assert.Contains(t, harness.readFrame(), "New workflow name", "and takes it back")
	})
}

// TestFormsFitTheWindow covers a form opening at its top and staying inside the window.
func TestFormsFitTheWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		height    int
		isRefused bool
		want      string
	}{
		{
			name:      "a refused form in a short window",
			height:    25,
			isRefused: true,
			want:      "Could not create the workflow",
		},
		{
			name:      "a refused form in a middling window",
			height:    32,
			isRefused: true,
			want:      "Could not create the workflow",
		},
		{
			name:      "a refused form in a tall window",
			height:    40,
			isRefused: true,
			want:      "Could not create the workflow",
		},
		{
			name:   "a form in a very short window",
			height: 16,
			want:   "New workflow name",
		},
		{
			name:   "a form in a short window",
			height: 24,
			want:   "New workflow name",
		},
		{
			name:   "a form in a middling window",
			height: 32,
			want:   "New workflow name",
		},
		{
			name:   "a form in a very tall window",
			height: 60,
			want:   "New workflow name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := newPanelHarness(t, panelHarnessParams{width: 160, height: tt.height})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(newFormWorkflow()))
			press := []string{"space", "w"}
			if tt.isRefused {
				harness.refuse(workflowv1connect.WorkflowServiceCreateWorkflowProcedure, connect.NewError(connect.CodeInvalidArgument, errors.New("no such workflow")))
				press = slices.Concat(press, spellKeys("web"), []string{"enter"})
			}

			harness.press(press...)

			frame := harness.readFrame()
			assert.LessOrEqual(t, len(splitLines(frame)), tt.height)
			assert.Contains(t, frame, tt.want)
		})
	}
}

func openForms(t *testing.T, workflow *workflowv1.Workflow, author *agentv1.Agent) *panelHarness {
	t.Helper()

	harness := openAgents(t, workflow, author)
	harness.press("g", "g")

	return harness
}

func openTaskForms(t *testing.T, author *agentv1.Agent) *panelHarness {
	t.Helper()

	harness := openAgents(t, newFormWorkflow(), author, newTaskOf(author))
	harness.press("g", "g")

	return harness
}

func openWideForms(t *testing.T, workflow *workflowv1.Workflow, author *agentv1.Agent) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{width: 220})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(workflow, author))
	harness.press("g", "g")

	return harness
}

func openFieldEditor(t *testing.T) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{settings: ui.PanelSettings{Editor: "true"}})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newConfiguredWorkflow(), newFormAuthor()))
	harness.press("g", "g")
	t.Cleanup(func() {
		for _, key := range []string{"context", "name", "worktree_copy_paths"} {
			_ = os.Remove(ui.FormFieldDraftPath(key))
		}
	})

	return harness
}

func openHookForm(t *testing.T, hooks []*workflowv1.WorkflowHook, available ...string) *panelHarness {
	t.Helper()

	workflow := newFormWorkflow()
	workflow.Hooks = hooks
	harness := openWideForms(t, workflow, newFormAuthor())
	harness.answer(hookv1connect.HookServiceListHooksProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return connect.NewResponse(&hookv1.ListHooksResponse{Hooks: xslice.Map(available, func(name string) *hookv1.Hook {
			return &hookv1.Hook{Name: name, Executable: true}
		})}), nil
	})

	harness.press(append([]string{"space", "E"}, openWorkflowSectionKeys(2)...)...)

	return harness
}

const workflowTopFieldCount = 4

func openWorkflowSectionKeys(index int) []string {
	return slices.Concat(slices.Repeat([]string{"tab"}, workflowTopFieldCount), slices.Repeat([]string{"down"}, index), []string{"enter"})
}

func assertSentHooks(t *testing.T, harness *panelHarness, want []*workflowv1.WorkflowHook) {
	t.Helper()

	sent := harness.readSent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure)
	require.Len(t, sent, 1)
	update, ok := sent[0].(*workflowv1.UpdateWorkflowRequest)
	require.True(t, ok)
	assert.Empty(t, cmp.Diff(want, update.GetHooks(), protocmp.Transform()))
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
	author := newAgent("a1", "author", "w1")
	author.Cwd = "/work/api"
	author.Harness = "claude"
	author.Flags = "--model opus"

	return author
}

func newLiveAuthor() *agentv1.Agent {
	author := newAgent("a1", "author", "w1")
	author.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	author.PaneId = "%9"
	author.Alive = xptr.Ptr(true)

	return author
}

func spellKeys(text string) []string {
	return strings.Split(text, "")
}

var ignoringEventPrompts = []cmp.Option{protocmp.IgnoreFields(&workflowv1.CreateWorkflowRequest{}, "event_prompts")}
