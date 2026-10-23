package ui_test

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	worktreev1 "komrad/proto/gen/worktree/v1"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestArchivedAgentsMenu covers what each entry of the archive menu does on the row under the cursor.
func TestArchivedAgentsMenu(t *testing.T) {
	neverStarted := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	neverStarted.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
	deletedAgent := paneltest.ArchivedAgent("a1", "api-refactor", "w1")

	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		watch      []*watchv1.WatchEvent
		wantScreen screen.ID
		wantFrame  string
		wantSent   []paneltest.Request
	}{
		{
			name:       "restore with no archived agents asks for no write",
			rows:       []proto.Message{paneltest.Workflow("w1", "backend")},
			press:      []string{"space", "a", "r"},
			wantScreen: screen.Agents,
		},
		{
			name:       "delete with no archived agents opens no prompt",
			rows:       []proto.Message{paneltest.Workflow("w1", "backend")},
			press:      []string{"space", "a", "d"},
			wantScreen: screen.Agents,
		},
		{
			name:       "restore on a live folder's header asks for no write",
			rows:       buildOneArchivedAgent(),
			press:      []string{"space", "a", "r"},
			wantScreen: screen.Agents,
		},
		{
			name:       "delete on a live folder's header opens no prompt",
			rows:       buildOneArchivedAgent(),
			press:      []string{"space", "a", "d"},
			wantScreen: screen.Agents,
		},
		{
			name:       "restore and open passes over a folder",
			rows:       buildOneArchivedAgent(),
			press:      []string{"space", "a", "o"},
			wantScreen: screen.Agents,
		},
		{
			name:       "restore and open passes over an agent that never started",
			rows:       []proto.Message{paneltest.Workflow("w1", "backend"), neverStarted},
			press:      []string{"j", "space", "a", "o", "ctrl+g"},
			wantScreen: screen.Agents,
			wantFrame:  "no conversation to open",
		},
		{
			name: "restore and open refuses a selection, which is not one agent",
			rows: []proto.Message{
				paneltest.Workflow("w1", "backend"),
				paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
				paneltest.ArchivedAgent("a2", "db-tune", "w1"),
			},
			press:      []string{"j", "v", "j", "v", "space", "a", "o", "ctrl+g"},
			wantScreen: screen.Agents,
			wantFrame:  "restore and open works on one agent at a time",
		},
		{
			name:       "back opens the view switcher",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "esc", "tab"},
			wantScreen: screen.Schedules,
		},
		{
			name:       "restore puts the hovered agent back and stays on the archived agents",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "space", "a", "r"},
			wantScreen: screen.Agents,
			wantSent: []paneltest.Request{{
				Procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				Msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:       "restore and open puts the agent back and opens it",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "space", "a", "o"},
			watch:      []*watchv1.WatchEvent{paneltest.Puts(newRestoredAgent())},
			wantScreen: screen.Agents,
			wantSent: []paneltest.Request{
				{
					Procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
					Msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
				},
				{
					Procedure: agentv1connect.AgentServiceResumeAgentProcedure,
					Msg:       &agentv1.ResumeAgentRequest{Id: "a1"},
				},
			},
		},
		{
			name:       "restore on an archived folder restores the workflow",
			rows:       []proto.Message{paneltest.ArchivedWorkflow("w1", "backend")},
			press:      []string{"space", "a", "r"},
			wantScreen: screen.Agents,
			wantSent: []paneltest.Request{{
				Procedure: workflowv1connect.WorkflowServiceRestoreWorkflowProcedure,
				Msg:       &workflowv1.RestoreWorkflowRequest{Id: "w1"},
			}},
		},
		{
			name:       "restore on a folder of the deleted list restores the workflow",
			rows:       []proto.Message{paneltest.ArchivedWorkflow("w1", "backend"), deletedAgent, newDeletedTaskOf(deletedAgent)},
			press:      []string{"F", "tab", "esc", "space", "u"},
			wantScreen: screen.Agents,
			wantSent: []paneltest.Request{{
				Procedure: workflowv1connect.WorkflowServiceRestoreWorkflowProcedure,
				Msg:       &workflowv1.RestoreWorkflowRequest{Id: "w1"},
			}},
		},
		{
			name:       "delete on a task opens the confirm prompt rather than destroying",
			rows:       buildArchivedTasks(),
			press:      []string{"j", "j", "j", "space", "a", "d"},
			wantScreen: screen.Agents,
			wantFrame:  `Delete task "db-work"?`,
		},
		{
			name:       "delete on an archived folder opens the confirm prompt",
			rows:       []proto.Message{paneltest.ArchivedWorkflow("w1", "backend")},
			press:      []string{"space", "a", "d"},
			wantScreen: screen.Agents,
			wantFrame:  `Delete workflow "backend"?`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, tt.rows...)
			harness.AnswerResume()

			harness.Press(tt.press...)
			if len(tt.watch) > 0 {
				harness.Watch(tt.watch...)
			}

			frame := harness.Frame()
			assert.Equal(t, tt.wantScreen, paneltest.Screen(harness.Frame()))
			if tt.wantFrame == "" {
				assert.NotContains(t, paneltest.StageProse(frame), deleteTitle)
			} else {
				assert.Contains(t, paneltest.StageProse(frame), tt.wantFrame)
			}
			harness.AssertSent(tt.wantSent...)
		})
	}
}

// TestArchivedAgentsPrompts covers answering the delete prompts.
func TestArchivedAgentsPrompts(t *testing.T) {
	archivedWithWorktree := paneltest.ArchivedWorkflow("w1", "backend")
	archivedWithWorktree.Worktree = &workflowv1.WorktreeConfig{Enabled: true}
	deleted := paneltest.ArchivedAgent("a1", "api-refactor", "w1")

	tests := []struct {
		name     string
		rows     []proto.Message
		press    []string
		gone     *watchv1.WatchEvent
		wantSent []paneltest.Request
	}{
		{
			name:  "declining the delete destroys nothing",
			rows:  buildArchivedTasks(),
			press: []string{"j", "j", "j", "space", "a", "d", "tab", "n"},
		},
		{
			name:  "declining the workflow delete destroys nothing",
			rows:  []proto.Message{paneltest.ArchivedWorkflow("w1", "backend")},
			press: []string{"space", "a", "d", "tab", "n"},
		},
		{
			name:  "the delete on a task waits for its agents to leave the archive",
			rows:  buildArchivedTasks(),
			press: []string{"j", "j", "j", "space", "a", "d", "enter"},
			gone:  paneltest.Deleted(watchv1.Resource_RESOURCE_AGENT, "a3"),
			wantSent: []paneltest.Request{
				{
					Procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
					Msg:       &taskv1.DeleteTasksRequest{TaskIds: []string{"t2"}, Records: true},
				},
				{
					Procedure: agentv1connect.AgentServiceGetLastScreenProcedure,
					Msg:       &agentv1.GetLastScreenRequest{AgentId: "a1"},
				},
			},
		},
		{
			name:  "confirming the workflow delete destroys the workflow it was opened on",
			rows:  []proto.Message{paneltest.ArchivedWorkflow("w1", "backend")},
			press: []string{"space", "a", "d", "enter"},
			gone:  paneltest.Deleted(watchv1.Resource_RESOURCE_WORKFLOW, "w1"),
			wantSent: []paneltest.Request{{
				Procedure: workflowv1connect.WorkflowServiceDeleteWorkflowProcedure,
				Msg:       &workflowv1.DeleteWorkflowRequest{Id: "w1", Records: true},
			}},
		},
		{
			name:  "a workflow delete of the worktree alone closes without waiting, since the workflow stays archived",
			rows:  []proto.Message{archivedWithWorktree},
			press: []string{"space", "a", "d", "enter"},
			wantSent: []paneltest.Request{{
				Procedure: workflowv1connect.WorkflowServiceDeleteWorkflowProcedure,
				Msg:       &workflowv1.DeleteWorkflowRequest{Id: "w1"},
			}},
		},
		{
			name:  "a workflow delete on the deleted list takes the records, since every worktree is already gone",
			rows:  []proto.Message{archivedWithWorktree, deleted, newDeletedTaskOf(deleted)},
			press: []string{"F", "tab", "esc", "space", "d", "enter"},
			gone:  paneltest.Deleted(watchv1.Resource_RESOURCE_WORKFLOW, "w1"),
			wantSent: []paneltest.Request{{
				Procedure: workflowv1connect.WorkflowServiceDeleteWorkflowProcedure,
				Msg:       &workflowv1.DeleteWorkflowRequest{Id: "w1", Records: true},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, tt.rows...)

			harness.Press(tt.press...)
			if tt.gone != nil {
				require.Contains(t, paneltest.StageProse(harness.Frame()), deleteTitle, "the prompt waits for the row to leave")
				harness.Watch(tt.gone)
			}

			frame := harness.Frame()
			assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()), "the prompt returns to the archived agents it was opened from")
			assert.NotContains(t, paneltest.StageProse(frame), deleteTitle, "the prompt is down")
			harness.AssertSent(tt.wantSent...)
		})
	}
}

// TestArchivedAgentsReportsRefusedWrites covers the banner a refused write leaves.
func TestArchivedAgentsReportsRefusedWrites(t *testing.T) {
	tests := []struct {
		name        string
		rows        []proto.Message
		refused     string
		press       []string
		wantSummary string
	}{
		{
			name:        "a refused restore",
			rows:        buildOneArchivedAgent(),
			refused:     agentv1connect.AgentServiceRestoreAgentsProcedure,
			press:       []string{"j", "space", "a", "r"},
			wantSummary: `Could not restore agent "api-refactor"`,
		},
		{
			name:        "a refused workflow restore",
			rows:        []proto.Message{paneltest.ArchivedWorkflow("w1", "backend")},
			refused:     workflowv1connect.WorkflowServiceRestoreWorkflowProcedure,
			press:       []string{"space", "a", "r"},
			wantSummary: `Could not restore workflow "backend"`,
		},
		{
			name:        "a refused restore and open",
			rows:        buildOneArchivedAgent(),
			refused:     agentv1connect.AgentServiceRestoreAgentsProcedure,
			press:       []string{"j", "space", "a", "o"},
			wantSummary: `Could not restore and open agent "api-refactor"`,
		},
		{
			name:        "a refused delete",
			rows:        buildArchivedTasks(),
			refused:     taskv1connect.TaskServiceDeleteTasksProcedure,
			press:       []string{"j", "j", "j", "space", "a", "d", "enter"},
			wantSummary: `Could not delete agent "db-tune"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, tt.rows...)
			harness.Refuse(tt.refused, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

			harness.Press(append(tt.press, paneltest.WidenListPresses...)...)

			assert.Contains(t, paneltest.EngineLine(harness.Frame()), tt.wantSummary)
			assert.Contains(t, harness.ReadMessages(), "no such agent")
		})
	}
}

// TestArchivedAgentsRows covers which folders and agents the archived and deleted kinds render.
func TestArchivedAgentsRows(t *testing.T) {
	kept := paneltest.ArchivedAgent("a1", "auth", "w1")
	deleted := paneltest.ArchivedAgent("a2", "billing", "w1")
	orchestrator := &agentv1.Agent{
		Id:         "o1",
		Name:       "orchestrator",
		WorkflowId: "w1",
		Role:       agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
		ArchivedAt: paneltest.ArchivedAt,
	}
	keptAndDeleted := []proto.Message{paneltest.ArchivedWorkflow("w1", "backend"), kept, newArchivedTaskOf(kept), deleted, newDeletedTaskOf(deleted)}
	everyDeleted := []proto.Message{paneltest.ArchivedWorkflow("w1", "backend"), orchestrator, deleted, newDeletedTaskOf(deleted)}

	tests := []struct {
		name     string
		rows     []proto.Message
		deleted  bool
		wantRows []string
	}{
		{
			name:     "nothing archived renders nothing",
			rows:     []proto.Message{paneltest.Workflow("w1", "backend")},
			wantRows: []string{},
		},
		{
			name:     "an archived folder emptied of its agents keeps its header",
			rows:     []proto.Message{paneltest.ArchivedWorkflow("w1", "backend")},
			wantRows: []string{"backend"},
		},
		{
			name: "a live folder with nothing put away out of it gets no header",
			rows: []proto.Message{
				paneltest.Workflow("w1", "backend"),
				paneltest.Workflow("w2", "frontend"),
				paneltest.ArchivedAgent("a1", "auth", "w1"),
			},
			wantRows: []string{"backend", "└─ auth"},
		},
		{
			name: "an orchestrator folds onto its folder",
			rows: []proto.Message{
				paneltest.Workflow("w1", "backend"),
				&agentv1.Agent{
					Id:         "o1",
					Name:       "orchestrator",
					WorkflowId: "w1",
					Role:       agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
					ArchivedAt: paneltest.ArchivedAt,
				},
			},
			wantRows: []string{"backend"},
		},
		{
			name: "live and archived folders are rendered together by name",
			rows: []proto.Message{
				paneltest.Workflow("w2", "frontend"),
				paneltest.ArchivedWorkflow("w1", "backend"),
				paneltest.ArchivedWorkflow("w3", "infra"),
				paneltest.ArchivedAgent("a1", "auth", "w1"),
				paneltest.ArchivedAgent("a2", "ui", "w2"),
			},
			wantRows: []string{"backend", "└─ auth", "frontend", "└─ ui", "infra"},
		},
		{
			name:     "a task whose worktree was deleted leaves the archived list",
			rows:     keptAndDeleted,
			wantRows: []string{"backend", "└─ task-a1"},
		},
		{
			name:     "the deleted list holds a task whose worktree was deleted",
			rows:     keptAndDeleted,
			deleted:  true,
			wantRows: []string{"backend", "└─ task-a2"},
		},
		{
			name:     "an archived folder whose every task was deleted goes to the deleted list with its orchestrator",
			rows:     everyDeleted,
			deleted:  true,
			wantRows: []string{"backend", "└─ task-a2"},
		},
		{
			name:     "an archived folder whose every task was deleted leaves the archived list",
			rows:     everyDeleted,
			wantRows: []string{},
		},
		{
			name:     "an archived folder with no tasks stays off the deleted list",
			rows:     []proto.Message{paneltest.ArchivedWorkflow("w1", "backend")},
			deleted:  true,
			wantRows: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, tt.rows...)
			if tt.deleted {
				harness.Press("F", "tab", "esc")
			}

			assert.Equal(t, tt.wantRows, paneltest.ListRows(harness.Frame()))
		})
	}
}

// TestArchivedAgentsMenuEntries covers the archive menu naming what the hovered row takes.
func TestArchivedAgentsMenuEntries(t *testing.T) {
	neverStarted := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	neverStarted.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
	openOff := paneltest.OffEntry("restore and open", "o")
	archiveEntry := regexp.MustCompile(`archive ›\s+a`)

	tests := []struct {
		name     string
		rows     []proto.Message
		press    []string
		want     []string
		wantOff  bool
		wantNone bool
	}{
		{
			name:     "on a live folder",
			rows:     buildOneArchivedAgent(),
			wantNone: true,
		},
		{
			name:    "on an archived folder",
			rows:    []proto.Message{paneltest.ArchivedWorkflow("w1", "backend")},
			want:    []string{"restore", "delete for good"},
			wantOff: true,
		},
		{
			name:    "on an agent that never started",
			rows:    []proto.Message{paneltest.Workflow("w1", "backend"), neverStarted},
			press:   []string{"j"},
			want:    []string{"restore", "delete for good"},
			wantOff: true,
		},
		{
			name:  "on an agent",
			rows:  buildOneArchivedAgent(),
			press: []string{"j"},
			want:  []string{"restore", "restore and open", "delete for good"},
		},
		{
			name:    "on a selection",
			rows:    buildOneArchivedAgent(),
			press:   []string{"j", "v"},
			want:    []string{"restore all", "delete all for good"},
			wantOff: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, tt.rows...)

			harness.Press(append(tt.press, "space")...)
			if tt.wantNone {
				assert.NotRegexp(t, archiveEntry, harness.Frame())

				return
			}
			require.Regexp(t, archiveEntry, harness.Frame())

			harness.Press("a")
			frame := harness.Frame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			if tt.wantOff {
				assert.Regexp(t, openOff, harness.RawFrame())
			} else {
				assert.NotRegexp(t, openOff, harness.RawFrame())
			}
		})
	}

	t.Run("an archived folder's menu holds none of its orchestrator's entries", func(t *testing.T) {
		orchestrator := paneltest.ArchivedAgent("o1", "orchestrator", "w1")
		orchestrator.Role = agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR
		harness := paneltest.OpenArchivedAgents(t, paneltest.ArchivedWorkflow("w1", "backend"), orchestrator)

		harness.Press("space")

		menu := strings.Join(paneltest.Stage(harness.Frame()), "\n")
		require.Regexp(t, archiveEntry, menu)
		for _, entry := range []string{"logs", "terminal", "jump ›"} {
			assert.NotContains(t, menu, entry)
		}
	})

	deletedAgent := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	onDeleted := []proto.Message{paneltest.ArchivedWorkflow("w1", "backend"), deletedAgent, newDeletedTaskOf(deletedAgent)}

	for _, tt := range []struct {
		name    string
		press   []string
		want    []*regexp.Regexp
		wantNot []*regexp.Regexp
	}{
		{
			name: "on a folder of the deleted list",
			want: []*regexp.Regexp{regexp.MustCompile(`restore\s+u`), regexp.MustCompile(`delete for good\s+d`)},
		},
		{
			name:    "on a task of the deleted list",
			press:   []string{"j"},
			want:    []*regexp.Regexp{regexp.MustCompile(`delete for good\s+d`)},
			wantNot: []*regexp.Regexp{regexp.MustCompile(`restore`)},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, onDeleted...)

			harness.Press(append(append([]string{"F", "tab", "esc"}, tt.press...), "space")...)

			menu := strings.Join(paneltest.Stage(harness.Frame()), "\n")
			assert.NotRegexp(t, archiveEntry, menu)
			for _, want := range tt.want {
				assert.Regexp(t, want, menu)
			}
			for _, wantNot := range tt.wantNot {
				assert.NotRegexp(t, wantNot, menu)
			}
		})
	}
}

// TestArchivedAgentsRename covers a name typed over an archived agents row going out as its rename.
func TestArchivedAgentsRename(t *testing.T) {
	archived := []proto.Message{
		paneltest.ArchivedWorkflow("w1", "backend"),
		paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
		&schedulev1.Schedule{Id: "s2", Name: "weekly-gone", WorkflowId: "w1", Cron: "0 9 * * *", ArchivedAt: paneltest.ArchivedAt},
	}
	live := []proto.Message{
		paneltest.Workflow("w1", "backend"),
		paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
	}

	tests := []struct {
		name      string
		rows      []proto.Message
		press     []string
		procedure string
		want      proto.Message
	}{
		{
			name:      "an archived agent",
			rows:      archived,
			press:     slices.Concat([]string{"j", "r"}, strings.Split("-2", ""), []string{"enter"}),
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
			want:      &agentv1.UpdateAgentRequest{Id: "a1", Name: "api-refactor-2"},
		},
		{
			name:      "a live workflow's folder",
			rows:      live,
			press:     slices.Concat([]string{"r"}, strings.Split("-2", ""), []string{"enter"}),
			procedure: workflowv1connect.WorkflowServiceUpdateWorkflowProcedure,
			want: &workflowv1.UpdateWorkflowRequest{
				Id:       "w1",
				Name:     "backend-2",
				Worktree: &workflowv1.WorktreeConfig{},
			},
		},
		{
			name:  "an archived workflow's folder takes no writes, so opens no field",
			rows:  archived,
			press: []string{"r"},
		},
		{
			name:  "an archived schedule takes no writes, so opens no field",
			rows:  archived,
			press: []string{"F", "tab", "esc", "r"},
		},
		{
			name:  "a selection opens no field",
			rows:  archived,
			press: []string{"j", "v", "r"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, tt.rows...)

			harness.Press(tt.press...)

			assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
			if tt.want == nil {
				assert.NotContains(t, harness.Frame(), "▏")
				harness.AssertSent()

				return
			}
			harness.AssertSentTo(tt.procedure, tt.want)
		})
	}
}

// TestArchivedAgentsDocument covers what the panel renders beside the list for the hovered row.
func TestArchivedAgentsDocument(t *testing.T) {
	stuck := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	stuck.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
	stuck.FailureReason = "the worktree is dirty"
	measured := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	measuredTask := paneltest.TaskOf(measured)
	measuredTask.DiffStat = &taskv1.DiffStat{Added: 200, Removed: 100}
	measuredTask.ArchivedAt = paneltest.ArchivedAt
	measuredReview := paneltest.ArchivedAgent("a2", "api-review", "w1")
	measuredReview.TaskId = xptr.Ptr(measuredTask.GetId())
	measuredReview.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER

	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		want       []string
		wantAbsent []string
	}{
		{
			name: "no archived agents says what the kind is for",
			rows: []proto.Message{paneltest.Workflow("w1", "backend")},
			want: []string{"nothing archived", "until you delete them"},
		},
		{
			name:       "an agent a delete could not destroy says why",
			rows:       []proto.Message{paneltest.Workflow("w1", "backend"), stuck},
			press:      []string{"j"},
			want:       []string{"teardown failed", "the worktree is dirty"},
			wantAbsent: []string{"Restore brings it back"},
		},
		{
			name:       "an archived agent is not one merely waiting to be opened",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j"},
			want:       []string{"archived — kept until deleted", "Restore brings it back"},
			wantAbsent: []string{"dormant"},
		},
		{
			name:       "an archived reviewer renders no diff stat",
			rows:       []proto.Message{paneltest.Workflow("w1", "backend"), measured, measuredTask, measuredReview},
			press:      []string{"j", "j"},
			want:       []string{"archived — kept until deleted"},
			wantAbsent: []string{"+200"},
		},
		{
			name:       "an archived task renders no diff stat",
			rows:       []proto.Message{paneltest.Workflow("w1", "backend"), measured, measuredTask},
			press:      []string{"j"},
			want:       []string{"task-a1", "no goal set"},
			wantAbsent: []string{"+200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, tt.rows...)

			harness.Press(tt.press...)

			frame := harness.Frame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
		})
	}

	t.Run("the record says what the agents list would say of it", func(t *testing.T) {
		agent := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
		agent.CreatedAt = timestamppb.New(time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC))
		task := paneltest.TaskOf(agent)
		task.Branch = xptr.Ptr("agent/a1")
		task.RepositoryUrl = xptr.Ptr("https://github.com/harrisoncramer/komrad")
		task.CreatedAt = timestamppb.New(time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC))
		task.ArchivedAt = paneltest.ArchivedAt
		harness := paneltest.OpenArchivedAgents(t,
			paneltest.Workflow("w1", "backend"),
			agent,
			task,
			&pullrequestv1.PullRequest{
				Id:                "pr-42",
				RepositoryUrl:     "https://github.com/harrisoncramer/komrad",
				HeadRepositoryUrl: "https://github.com/harrisoncramer/komrad",
				HeadBranch:        "agent/a1",
				Number:            42,
				State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
				OpenedAt:          timestamppb.New(time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)),
				MergedAt:          paneltest.ArchivedAt,
				ClosedAt:          paneltest.ArchivedAt,
			},
		)

		harness.Press(append([]string{"j", "pick:record"}, paneltest.WidenListPresses...)...)

		frame := harness.Frame()
		assert.Contains(t, frame, "agent/a1", "the task's record carries its checkout")
		assert.Contains(t, frame, "its pull request is merged")
		created, archived := paneltest.RecordLine(frame, "created"), paneltest.RecordLine(frame, "archived")
		require.Positive(t, created)
		assert.Less(t, created, archived, "when it was put away comes after when it was made")
	})
}

// TestArchivedAgentsSort covers ordering the archived agents by their column tabs.
func TestArchivedAgentsSort(t *testing.T) {
	unsorted := []proto.Message{
		paneltest.Workflow("w1", "backend"),
		newArchivedAgentAt("zeta", time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)),
		newArchivedAgentAt("mid", time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)),
		newArchivedAgentAt("alpha", time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)),
	}

	tests := []struct {
		name  string
		press []string
		want  []string
	}{
		{
			name: "archived down is the fallback, the most recently archived first",
			want: []string{"backend", "├─ mid", "├─ zeta", "└─ alpha"},
		},
		{
			name:  "archived up is the oldest first",
			press: []string{"f", "k"},
			want:  []string{"backend", "├─ alpha", "├─ zeta", "└─ mid"},
		},
		{
			name:  "name up takes every row",
			press: []string{"f", "tab", "k"},
			want:  []string{"backend", "├─ alpha", "├─ mid", "└─ zeta"},
		},
		{
			name:  "name down",
			press: []string{"f", "tab", "k", "j"},
			want:  []string{"backend", "├─ zeta", "├─ mid", "└─ alpha"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, unsorted...)

			harness.Press(tt.press...)

			assert.Equal(t, tt.want, paneltest.ListRows(harness.Frame()))
		})
	}
}

// TestArchivedAgentsWindow covers the archived agents holding the window and the keyboard.
func TestArchivedAgentsWindow(t *testing.T) {
	t.Run("entering the archived agents asks for nothing", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, buildOneArchivedAgent()...)

		assert.Contains(t, harness.Frame(), "api-refactor")
		harness.AssertSent()
	})

	t.Run("the archived agents take the window and gives it back for a running agent", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t,
			paneltest.Workflow("w1", "backend"),
			&agentv1.Agent{
				Id:         "a2",
				Name:       "auth",
				WorkflowId: "w1",
				PaneId:     "%9",
				Alive:      xptr.Ptr(true),
				State:      agentv1.AgentState_AGENT_STATE_IDLE,
			},
			paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
		)
		require.True(t, harness.Tmux().IsZoomed(), "the archived agents render both columns themselves")

		harness.Press("F", "shift+tab", "esc", "j")
		harness.Advance(time.Second)

		assert.False(t, harness.Tmux().IsZoomed(), "leaving the archived agents for a running agent gives the other panes their share back")
	})

	t.Run("restoring and opening an agent hands the window to the pane it opens", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, buildOneArchivedAgent()...)
		require.True(t, harness.Tmux().IsZoomed())
		harness.AnswerResume()

		harness.Press("j", "space", "a", "o")
		harness.Watch(paneltest.Puts(newRestoredAgent()))

		assert.False(t, harness.Tmux().IsZoomed(), "the pane it made is not left under the archived agents' own zoom")
		assert.Equal(t, "%9", harness.Tmux().GuestPane())
	})
}

// TestArchivedAgentsSelection covers choosing several archived agents and acting on them at once.
func TestArchivedAgentsSelection(t *testing.T) {
	rows := []proto.Message{
		paneltest.Workflow("w1", "backend"),
		newArchivedAgentAt("auth", time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)),
		newArchivedAgentAt("api", time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)),
		newArchivedAgentAt("db", time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)),
	}

	tests := []struct {
		name     string
		press    []string
		wantSent []paneltest.Request
	}{
		{
			name:  "restore takes the whole selection rather than the hovered row",
			press: []string{"j", "v", "j", "v", "j", "space", "a", "r"},
			wantSent: []paneltest.Request{{
				Procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				Msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"auth", "api"}},
			}},
		},
		{
			name:  "delete is disabled on a selection of agents with no task",
			press: []string{"j", "v", "j", "v", "j", "space", "a", "d", "enter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, rows...)

			harness.Press(tt.press...)

			assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
			harness.AssertSent(tt.wantSent...)
		})
	}

	t.Run("a folder restore stands down while a selection is up", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, paneltest.ArchivedWorkflow("w1", "backend"), paneltest.ArchivedAgent("a1", "auth", "w1"))

		harness.Press("j", "v", "g", "g", "space", "a", "r")

		harness.AssertSent(paneltest.Request{
			Procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
			Msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
		})
	})

	t.Run("the selection outlives a refused restore", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, rows...)
		harness.Refuse(agentv1connect.AgentServiceRestoreAgentsProcedure, connect.NewError(connect.CodeUnavailable, errors.New("down")))

		harness.Press("j", "v", "space", "a", "r")

		assert.Contains(t, paneltest.Legend(harness.Frame()), "1 selected")
	})

	t.Run("escape clears the selection before it leaves the archived agents", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, rows...)
		harness.Press("j", "v")

		harness.Press("esc")

		frame := harness.Frame()
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.NotContains(t, paneltest.Legend(frame), "selected")

		harness.Press("F", "tab", "esc")
		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
	})

	t.Run("an agent gone from the archived agents drops out of the selection", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, rows...)
		harness.Press("j", "v", "G", "v")

		harness.Watch(paneltest.Snapshot(rows[0], rows[3]))
		harness.Press("space", "a", "r")

		harness.AssertSentTo(agentv1connect.AgentServiceRestoreAgentsProcedure, &agentv1.RestoreAgentsRequest{Ids: []string{"db"}})
	})

	t.Run("selecting among the archived agents leaves the agents tree alone", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, append(slices.Clone(rows), paneltest.Agent("a9", "live", "w1"))...)
		harness.Press("j", "v", "esc", "F", "tab", "esc")

		assert.NotContains(t, paneltest.Legend(harness.Frame()), "selected")
	})
}

// TestArchivedAgentsKinds covers the row of kinds switching the agents view between its live, archived and deleted agents.
func TestArchivedAgentsKinds(t *testing.T) {
	deleted := paneltest.ArchivedAgent("a2", "billing", "w1")
	rows := []proto.Message{
		paneltest.Workflow("w1", "backend"),
		paneltest.Agent("a9", "auth-live", "w1"),
		paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
		deleted,
		newDeletedTaskOf(deleted),
	}

	tests := []struct {
		name       string
		press      []string
		want       string
		wantAbsent string
	}{
		{
			name:       "tab on the row of kinds switches to the archived agents",
			press:      []string{"F", "tab", "esc"},
			want:       "api-refactor",
			wantAbsent: "auth-live",
		},
		{
			name:       "tab again switches to the deleted agents",
			press:      []string{"F", "tab", "tab", "esc"},
			want:       "task-a2",
			wantAbsent: "api-refactor",
		},
		{
			name:       "tab past the deleted agents goes back to the live agents",
			press:      []string{"F", "tab", "tab", "tab", "esc"},
			want:       "auth-live",
			wantAbsent: "api-refactor",
		},
		{
			name:       "shift-tab on the row of kinds wraps to the deleted agents",
			press:      []string{"F", "shift+tab", "esc"},
			want:       "task-a2",
			wantAbsent: "auth-live",
		},
		{
			name:       "tab on the list leaves the kind alone",
			press:      []string{"F", "tab", "esc", "tab", "shift+tab"},
			want:       "api-refactor",
			wantAbsent: "auth-live",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenAgents(t, rows...)

			harness.Press(tt.press...)
			harness.Press(paneltest.WidenListPresses...)

			frame := ansi.Strip(harness.Frame())
			assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
			assert.Contains(t, frame, tt.want)
			assert.NotContains(t, frame, tt.wantAbsent)
		})
	}

	t.Run("the row of kinds refuses the keys while agents are selected", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, rows...)

		harness.Press(append(paneltest.WidenListPresses, "j", "v", "F", "?")...)
		frame := harness.Frame()
		assert.NotEmpty(t, paneltest.HoveredRow(frame), "the list keeps the keys")
		assert.NotContains(t, paneltest.Legend(frame), "F kind")
	})

	t.Run("the row of kinds holds the keys until escape", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, rows...)

		harness.Press(append(paneltest.WidenListPresses, "F")...)
		frame := harness.Frame()
		legend := paneltest.Legend(frame)
		assert.Contains(t, legend, "tab/S-tab switch")
		assert.Contains(t, legend, "esc back to the list")
		assert.Empty(t, paneltest.HoveredRow(frame), "the row's arrow stands down")

		harness.Press("esc")
		assert.NotEmpty(t, paneltest.HoveredRow(harness.Frame()), "and the list has the keys again")
	})

	t.Run("each kind comes back on the row it was left on", func(t *testing.T) {
		harness := paneltest.OpenAgents(t,
			paneltest.Workflow("w1", "backend"),
			paneltest.Agent("a8", "auth-live", "w1"),
			paneltest.Agent("a9", "billing-live", "w1"),
			paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
			paneltest.ArchivedAgent("a2", "billing", "w1"),
		)
		harness.Press(append(paneltest.WidenListPresses, "j")...)
		live := paneltest.HoveredRow(harness.Frame())
		harness.Press("F", "tab", "esc", "j")
		archived := paneltest.HoveredRow(harness.Frame())

		harness.Press("F", "shift+tab", "esc")
		assert.Equal(t, live, paneltest.HoveredRow(harness.Frame()))

		harness.Press("F", "tab", "esc")
		assert.Equal(t, archived, paneltest.HoveredRow(harness.Frame()))
	})
}

// TestArchivedAgentsLastScreen covers the saved screen rendered under an archived agent.
func TestArchivedAgentsLastScreen(t *testing.T) {
	t.Run("the saved screen is asked for once the cursor rests and rendered under the document", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, buildOneArchivedAgent()...)
		harness.AnswerLastScreens(map[string]string{"a1": "❯ ship the refactor\n⏺ the refactor is merged"})

		harness.Press("j")
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceGetLastScreenProcedure), "nothing is asked while the cursor moves")

		harness.Advance(time.Second)

		requireLastScreenFor(t, harness, "a1")
		frame := harness.Frame()
		for _, want := range []string{"archived · press space a o to restore", "Restore brings it back", "saved screen", "80x24", "❯ ship the refactor", "⏺ the refactor is merged"} {
			assert.Contains(t, frame, want)
		}
	})

	t.Run("a saved screen taller than the room left is fitted under the document and its banner", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, buildOneArchivedAgent()...)
		harness.Answer(agentv1connect.AgentServiceGetLastScreenProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.GetLastScreenResponse{
				Frame:      "❯ the first ask\n" + strings.Repeat("working through it\n", 60) + "⏺ the refactor is merged",
				Width:      80,
				Height:     62,
				CapturedAt: timestamppb.New(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)),
			}), nil
		})

		harness.Press("j")
		harness.Advance(time.Second)

		frame := harness.Frame()
		for _, want := range []string{"Restore brings it back", "saved screen", "⏺ the refactor is merged"} {
			assert.Contains(t, frame, want)
		}
		assert.NotContains(t, frame, "the first ask")
	})

	t.Run("an agent with no saved screen says so", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, buildOneArchivedAgent()...)
		harness.AnswerLastScreens(nil)

		harness.Press("j")
		harness.Advance(time.Second)

		assert.Contains(t, harness.Frame(), "no saved screen")
	})

	t.Run("a refused read says why", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, buildOneArchivedAgent()...)
		harness.Answer(agentv1connect.AgentServiceGetLastScreenProcedure, paneltest.RefuseWith(connect.CodeInternal, "screens exploded"))

		harness.Press("j")
		harness.Advance(time.Second)

		frame := harness.Frame()
		assert.Contains(t, frame, "could not read the saved screen: screens exploded")
		assert.Contains(t, frame, "Restore brings it back")
	})

	t.Run("a folder has no saved screen", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, buildOneArchivedAgent()...)
		harness.Advance(time.Second)

		assert.NotContains(t, harness.Frame(), "saved screen")
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceGetLastScreenProcedure))
	})

	t.Run("an answer for an agent the cursor has left is not rendered", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t,
			paneltest.Workflow("w1", "backend"),
			paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
			paneltest.ArchivedAgent("a2", "billing", "w1"),
		)
		harness.AnswerLastScreens(map[string]string{
			"a1": "words from api-refactor",
			"a2": "words from billing",
		})
		harness.Hold(agentv1connect.AgentServiceGetLastScreenProcedure)

		harness.Press("j")
		harness.Advance(time.Second)
		harness.Press("j")
		harness.Release(agentv1connect.AgentServiceGetLastScreenProcedure)

		frame := harness.Frame()
		assert.NotContains(t, frame, "words from api-refactor")
		assert.NotContains(t, frame, "words from billing")
		assert.Contains(t, frame, "reading the saved screen")
	})

	t.Run("clearing a search that moved the cursor asks for the agent it lands back on", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t,
			paneltest.Workflow("w1", "backend"),
			paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
			paneltest.ArchivedAgent("a2", "billing", "w1"),
		)
		harness.AnswerLastScreens(map[string]string{
			"a1": "words from api-refactor",
			"a2": "words from billing",
		})
		harness.Press("j")
		harness.Advance(time.Second)

		harness.Press("/", "a", "p", "i", "enter")
		harness.Advance(time.Second)
		require.Contains(t, harness.Frame(), "words from api-refactor")

		harness.Press("esc")
		harness.Advance(time.Second)

		assert.Contains(t, harness.Frame(), "words from billing")
	})

	t.Run("a listing that moves the cursor onto another agent asks for it", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t,
			paneltest.Workflow("w1", "backend"),
			paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
			paneltest.ArchivedAgent("a2", "billing", "w1"),
		)
		harness.AnswerLastScreens(nil)
		harness.Press("j")
		harness.Advance(time.Second)

		requireLastScreenFor(t, harness, "a2")

		restored := paneltest.ArchivedAgent("a2", "billing", "w1")
		restored.ArchivedAt = nil
		harness.Watch(paneltest.Puts(restored))

		requireLastScreenFor(t, harness, "a2", "a1")
	})
}

// TestArchivedAgentsReading covers the keys handed to an archived agent's record.
func TestArchivedAgentsReading(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = fmt.Sprintf("line number %d", i)
	}
	screen := strings.Join(lines, "\n")

	tests := []struct {
		name    string
		presses []string
		want    []string
		wantNot []string
	}{
		{
			name:    "the bottom of the focused record is the bottom of its saved screen",
			presses: []string{"pick:block", "G"},
			want:    []string{"line number 39"},
			wantNot: []string{"line number 15"},
		},
		{
			name:    "searching the record lands on what it found and counts it",
			presses: []string{"pick:block", "/", "3", "8", "enter"},
			want:    []string{"/ 38", "1 of 1", "line number 38"},
		},
		{
			name:    "leaving the record gives the keys back to the list",
			presses: []string{"pick:block", "G", "esc", "k"},
			want:    []string{"no shared context set"},
			wantNot: []string{"line number 39"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t,
				paneltest.Workflow("w1", "backend"),
				paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
				paneltest.ArchivedAgent("a2", "billing", "w1"),
			)
			harness.AnswerLastScreens(map[string]string{"a1": screen, "a2": screen})
			harness.Press("j")
			harness.Advance(time.Second)

			harness.Press(tt.presses...)

			frame := harness.Frame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantNot {
				assert.NotContains(t, frame, absent)
			}
		})
	}

	t.Run("the saved screen takes the keys while the live list stands on a stopped agent", func(t *testing.T) {
		stopped := paneltest.Dormant("sleepy")
		stopped.Id = "d1"
		harness := paneltest.Start(t, paneltest.Params{})
		harness.AnswerLastScreens(map[string]string{"a1": screen, "a2": screen})
		harness.Watch(paneltest.Snapshot(
			paneltest.Workflow("w1", "backend"),
			stopped,
			paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
			paneltest.ArchivedAgent("a2", "billing", "w1"),
		))
		harness.Press("F", "tab", "esc", "j")
		harness.Advance(time.Second)

		harness.Press("pick:block", "G")

		assert.Contains(t, harness.Frame(), "line number 39")
	})
}

func buildOneArchivedAgent() []proto.Message {
	return []proto.Message{
		paneltest.Workflow("w1", "backend"),
		paneltest.ArchivedAgent("a1", "api-refactor", "w1"),
	}
}

const deleteTitle = "Delete "

func newRestoredAgent() *agentv1.Agent {
	agent := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	agent.ArchivedAt = nil
	agent.PaneId = "%9"
	agent.Alive = xptr.Ptr(true)

	return agent
}

func newArchivedAgentAt(name string, at time.Time) *agentv1.Agent {
	agent := paneltest.ArchivedAgent(name, name, "w1")
	agent.ArchivedAt = dates.Stamp(at)

	return agent
}

func requireLastScreenFor(t *testing.T, harness *paneltest.Panel, agentIDs ...string) {
	t.Helper()

	sent := xslice.Map(harness.Sent(agentv1connect.AgentServiceGetLastScreenProcedure), func(msg proto.Message) string {
		read, _ := msg.(*agentv1.GetLastScreenRequest)

		return read.GetAgentId()
	})
	require.Equal(t, agentIDs, sent)
}

// TestArchivedTasks covers restore, delete and the menu on a task row, archived or partly archived.
func TestArchivedTasks(t *testing.T) {
	unstarted := buildArchivedTasks()
	for _, row := range unstarted {
		if agent, ok := row.(*agentv1.Agent); ok && agent.GetId() == "a1" {
			agent.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
		}
	}
	removed := buildArchivedTasks()
	for _, row := range removed {
		if task, ok := row.(*taskv1.Task); ok && task.GetId() == "t1" {
			task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		}
	}
	emptied := slices.DeleteFunc(buildArchivedTasks(), func(row proto.Message) bool {
		agent, ok := row.(*agentv1.Agent)

		return ok && agent.GetId() == "a5"
	})
	archivedWorkflow := paneltest.ArchivedWorkflow("w1", "backend")
	archivedWorkflow.Worktree = &workflowv1.WorktreeConfig{Enabled: true}
	inArchivedWorkflow := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	inArchivedWorkflowTask := paneltest.TaskOf(inArchivedWorkflow)
	inArchivedWorkflowTask.Cwd = xptr.Ptr("/trees/api")
	inArchivedWorkflowTask.ArchivedAt = paneltest.ArchivedAt
	goneAgent := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	goneTask := newArchivedTaskOf(goneAgent)
	goneTask.Cwd = xptr.Ptr("/trees/api")
	goneTask.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
	deletedAgent := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	deletedTask := newDeletedTaskOf(deletedAgent)
	deletedTask.Cwd = xptr.Ptr("/trees/api")
	onAPIWork := []string{"j", "j", "j", "j"}

	tests := []struct {
		name      string
		rows      []proto.Message
		press     []string
		wantFrame string
		wantSent  []paneltest.Request
	}{
		{
			name:  "restore on a task restores its writer alone",
			rows:  buildArchivedTasks(),
			press: append(onAPIWork, "space", "a", "r"),
			wantSent: []paneltest.Request{{
				Procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				Msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:  "restore and open on a task restores its writer and opens it",
			rows:  buildArchivedTasks(),
			press: append(onAPIWork, "space", "a", "o"),
			wantSent: []paneltest.Request{
				{
					Procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
					Msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
				},
				{
					Procedure: agentv1connect.AgentServiceResumeAgentProcedure,
					Msg:       &agentv1.ResumeAgentRequest{Id: "a1"},
				},
			},
		},
		{
			name:      "restore and open passes over a task whose writer never started",
			rows:      unstarted,
			press:     append(onAPIWork, "space", "a", "o", "ctrl+g"),
			wantFrame: "no conversation to open",
		},
		{
			name:      "restore and open passes over a task whose worktree was removed",
			rows:      removed,
			press:     append(onAPIWork, "space", "a", "o", "ctrl+g"),
			wantFrame: "no checkout to run in",
		},
		{
			name:  "delete on a task deletes the task",
			rows:  buildArchivedTasks(),
			press: append(onAPIWork, "space", "a", "d", "enter"),
			wantSent: []paneltest.Request{{
				Procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
				Msg:       &taskv1.DeleteTasksRequest{TaskIds: []string{"t1"}, Records: true},
			}},
		},
		{
			name:  "delete on a task under an archived workflow deletes its worktree alone by default",
			rows:  []proto.Message{archivedWorkflow, inArchivedWorkflow, inArchivedWorkflowTask},
			press: []string{"j", "space", "a", "d", "enter"},
			wantSent: []paneltest.Request{
				{
					Procedure: worktreev1connect.WorktreeServiceGetWorktreeProcedure,
					Msg:       &worktreev1.GetWorktreeRequest{Dir: "/trees/api"},
				},
				{
					Procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
					Msg:       &taskv1.DeleteTasksRequest{TaskIds: []string{"task-a1"}},
				},
			},
		},
		{
			name:  "delete on a task gone without a delete still offers its worktree, which records it as deleted",
			rows:  []proto.Message{archivedWorkflow, goneAgent, goneTask},
			press: []string{"j", "space", "a", "d", "enter"},
			wantSent: []paneltest.Request{{
				Procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
				Msg:       &taskv1.DeleteTasksRequest{TaskIds: []string{"task-a1"}},
			}},
		},
		{
			name:  "delete on a task whose worktree was deleted takes its records",
			rows:  []proto.Message{archivedWorkflow, deletedAgent, deletedTask},
			press: []string{"F", "tab", "esc", "j", "space", "d", "enter"},
			wantSent: []paneltest.Request{{
				Procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
				Msg:       &taskv1.DeleteTasksRequest{TaskIds: []string{"task-a1"}, Records: true},
			}},
		},
		{
			name:      "the delete prompt on a task names the task",
			rows:      buildArchivedTasks(),
			press:     append(onAPIWork, "space", "a", "d"),
			wantFrame: `Delete task "api-work"?`,
		},
		{
			name:  "restore on a live task's header restores only its archived agents",
			rows:  buildArchivedTasks(),
			press: []string{"j", "space", "a", "r"},
			wantSent: []paneltest.Request{{
				Procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				Msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a4"}},
			}},
		},
		{
			name:      "delete is disabled on a live task's header, since its archived reviewer will only be deleted with the task",
			rows:      buildArchivedTasks(),
			press:     []string{"j", "space", "a"},
			wantFrame: "will only be deleted with its task or workflow",
		},
		{
			name:  "delete is disabled on a live task with no live agent left",
			rows:  emptied,
			press: []string{"j", "space", "a", "d", "enter"},
		},
		{
			name:  "the range key takes the tasks between",
			rows:  buildArchivedTasks(),
			press: []string{"j", "j", "j", "v", "j", "V", "space", "a", "d", "enter"},
			wantSent: []paneltest.Request{{
				Procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
				Msg:       &taskv1.DeleteTasksRequest{TaskIds: []string{"t2", "t1"}, Records: true},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenArchivedAgents(t, tt.rows...)
			harness.AnswerWorktreeReads()
			harness.AnswerResume()

			harness.Press(tt.press...)

			if tt.wantFrame != "" {
				assert.Contains(t, harness.Frame(), tt.wantFrame)
			}
			harness.AssertSent(tt.wantSent...)
		})
	}

	t.Run("a task's menu leaves out what only a live task takes", func(t *testing.T) {
		harness := paneltest.OpenArchivedAgents(t, buildArchivedTasks()...)

		harness.Press(append(onAPIWork, "space")...)

		frame := harness.Frame()
		require.Regexp(t, `archive ›\s+a`, frame)
		assert.NotContains(t, frame, "hide")
		assert.NotContains(t, frame, "edit task")
	})
}

// buildArchivedTasks is two archived tasks and a live one with an agent archived out of it.
func buildArchivedTasks() []proto.Message {
	refactor := paneltest.ArchivedAgent("a1", "api-refactor", "w1")
	review := paneltest.ArchivedAgent("a2", "api-review", "w1")
	tune := paneltest.ArchivedAgent("a3", "db-tune", "w1")
	auth := paneltest.ArchivedAgent("a4", "auth", "w1")
	authLive := paneltest.Agent("a5", "auth-live", "w1")
	for _, agent := range []*agentv1.Agent{refactor, review} {
		agent.TaskId = xptr.Ptr("t1")
	}
	tune.TaskId = xptr.Ptr("t2")
	for _, agent := range []*agentv1.Agent{auth, authLive} {
		agent.TaskId = xptr.Ptr("t3")
	}
	for _, agent := range []*agentv1.Agent{refactor, tune, authLive} {
		agent.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	}
	for _, agent := range []*agentv1.Agent{review, auth} {
		agent.Role = agentv1.AgentRole_AGENT_ROLE_REVIEWER
	}

	return []proto.Message{
		paneltest.Workflow("w1", "backend"),
		refactor, review, tune, auth, authLive,
		&taskv1.Task{
			Id:         "t1",
			WorkflowId: "w1",
			Slug:       "api-work",
			ArchivedAt: paneltest.ArchivedAt,
		},
		&taskv1.Task{
			Id:         "t2",
			WorkflowId: "w1",
			Slug:       "db-work",
			ArchivedAt: paneltest.ArchivedAt,
		},
		&taskv1.Task{
			Id:         "t3",
			WorkflowId: "w1",
			Slug:       "auth-work",
		},
	}
}

func newArchivedTaskOf(agent *agentv1.Agent) *taskv1.Task {
	task := paneltest.TaskOf(agent)
	task.ArchivedAt = paneltest.ArchivedAt

	return task
}

func newDeletedTaskOf(agent *agentv1.Agent) *taskv1.Task {
	task := newArchivedTaskOf(agent)
	task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED

	return task
}
