package ui_test

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"
	"komrad/proto/gen/task/v1/taskv1connect"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestArchivedAgentsMenu covers what each entry of the archive menu does on the row under the cursor.
func TestArchivedAgentsMenu(t *testing.T) {
	neverStarted := newArchivedAgent("a1", "api-refactor", "w1")
	neverStarted.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE

	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		watch      []*watchv1.WatchEvent
		wantScreen string
		wantFrame  string
		wantSent   []request
	}{
		{
			name:       "restore with no archived agents asks for no write",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend")},
			press:      []string{"space", "a", "r"},
			wantScreen: agentsScreen,
		},
		{
			name:       "delete with no archived agents opens no prompt",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend")},
			press:      []string{"space", "a", "d"},
			wantScreen: agentsScreen,
		},
		{
			name:       "restore on a live folder's header asks for no write",
			rows:       buildOneArchivedAgent(),
			press:      []string{"space", "a", "r"},
			wantScreen: agentsScreen,
		},
		{
			name:       "delete on a live folder's header opens no prompt",
			rows:       buildOneArchivedAgent(),
			press:      []string{"space", "a", "d"},
			wantScreen: agentsScreen,
		},
		{
			name:       "restore and open passes over a folder",
			rows:       buildOneArchivedAgent(),
			press:      []string{"space", "a", "o"},
			wantScreen: agentsScreen,
		},
		{
			name:       "restore and open passes over an agent that never started",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend"), neverStarted},
			press:      []string{"j", "space", "a", "o", "ctrl+g"},
			wantScreen: agentsScreen,
			wantFrame:  "no conversation to open",
		},
		{
			name: "restore and open refuses a selection, which is not one agent",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newArchivedAgent("a1", "api-refactor", "w1"),
				newArchivedAgent("a2", "db-tune", "w1"),
			},
			press:      []string{"j", "v", "j", "v", "space", "a", "o", "ctrl+g"},
			wantScreen: agentsScreen,
			wantFrame:  "restore and open works on one agent at a time",
		},
		{
			name:       "back opens the view switcher",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "esc", "tab"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "restore puts the hovered agent back and stays on the archived agents",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "space", "a", "r"},
			wantScreen: agentsScreen,
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:       "restore and open puts the agent back and opens it",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "space", "a", "o"},
			watch:      []*watchv1.WatchEvent{buildPuts(newRestoredAgent())},
			wantScreen: agentsScreen,
			wantSent: []request{
				{
					procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
					msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
				},
				{
					procedure: agentv1connect.AgentServiceResumeAgentProcedure,
					msg:       &agentv1.ResumeAgentRequest{Id: "a1"},
				},
			},
		},
		{
			name:       "restore on an archived folder restores the workflow",
			rows:       []proto.Message{newArchivedWorkflow("w1", "backend")},
			press:      []string{"space", "a", "r"},
			wantScreen: agentsScreen,
			wantSent: []request{{
				procedure: workflowv1connect.WorkflowServiceRestoreWorkflowProcedure,
				msg:       &workflowv1.RestoreWorkflowRequest{Id: "w1"},
			}},
		},
		{
			name:       "delete on an agent opens the confirm prompt rather than destroying",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "space", "a", "d"},
			wantScreen: agentsScreen,
			wantFrame:  `Permanently delete agent "api-refactor"?`,
		},
		{
			name:       "delete on an archived folder opens the confirm prompt",
			rows:       []proto.Message{newArchivedWorkflow("w1", "backend")},
			press:      []string{"space", "a", "d"},
			wantScreen: agentsScreen,
			wantFrame:  `Permanently delete workflow "backend"?`,
		},
		{
			name:       "delete on an archived folder says the agent inside it goes too",
			rows:       []proto.Message{newArchivedWorkflow("w1", "backend"), newArchivedAgent("a1", "api-refactor", "w1")},
			press:      []string{"space", "a", "d"},
			wantScreen: agentsScreen,
			wantFrame:  "Komrad also deletes the one archived agent in it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchivedAgents(t, tt.rows...)
			answerResume(harness)

			harness.press(tt.press...)
			if len(tt.watch) > 0 {
				harness.watch(tt.watch...)
			}

			frame := harness.readFrame()
			assert.Equal(t, tt.wantScreen, harness.readScreen())
			if tt.wantFrame == "" {
				assert.NotContains(t, frame, deletePrompt)
			} else {
				assert.Contains(t, frame, tt.wantFrame)
			}
			harness.assertSent(tt.wantSent...)
		})
	}
}

// TestArchivedAgentsPrompts covers answering the delete prompts.
func TestArchivedAgentsPrompts(t *testing.T) {
	tests := []struct {
		name     string
		rows     []proto.Message
		press    []string
		gone     *watchv1.WatchEvent
		wantSent []request
	}{
		{
			name:  "declining the delete destroys nothing",
			rows:  buildOneArchivedAgent(),
			press: []string{"j", "space", "a", "d", "n"},
		},
		{
			name:  "declining the workflow delete destroys nothing",
			rows:  []proto.Message{newArchivedWorkflow("w1", "backend")},
			press: []string{"space", "a", "d", "n"},
		},
		{
			name:  "confirming the delete destroys the agent it was opened on",
			rows:  buildOneArchivedAgent(),
			press: []string{"j", "space", "a", "d", "y"},
			gone:  buildDeleted(watchv1.Resource_RESOURCE_AGENT, "a1"),
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
				msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:  "the delete on a task waits for its agents to leave the archive",
			rows:  buildArchivedTasks(),
			press: []string{"j", "j", "j", "space", "a", "d", "y"},
			gone:  buildDeleted(watchv1.Resource_RESOURCE_AGENT, "a3"),
			wantSent: []request{
				{
					procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
					msg:       &taskv1.DeleteTasksRequest{Tasks: []string{"t2"}},
				},
				{
					procedure: agentv1connect.AgentServiceGetLastScreenProcedure,
					msg:       &agentv1.GetLastScreenRequest{AgentId: "a1"},
				},
			},
		},
		{
			name:  "confirming the workflow delete destroys the workflow it was opened on",
			rows:  []proto.Message{newArchivedWorkflow("w1", "backend")},
			press: []string{"space", "a", "d", "y"},
			gone:  buildDeleted(watchv1.Resource_RESOURCE_WORKFLOW, "w1"),
			wantSent: []request{{
				procedure: workflowv1connect.WorkflowServiceDeleteWorkflowProcedure,
				msg:       &workflowv1.DeleteWorkflowRequest{Id: "w1"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchivedAgents(t, tt.rows...)

			harness.press(tt.press...)
			if tt.gone != nil {
				require.Contains(t, harness.readFrame(), deletePrompt, "the prompt waits for the row to leave")
				harness.watch(tt.gone)
			}

			frame := harness.readFrame()
			assert.Equal(t, agentsScreen, harness.readScreen(), "the prompt returns to the archived agents it was opened from")
			assert.NotContains(t, frame, deletePrompt, "the prompt is down")
			harness.assertSent(tt.wantSent...)
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
			rows:        []proto.Message{newArchivedWorkflow("w1", "backend")},
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
			rows:        buildOneArchivedAgent(),
			refused:     agentv1connect.AgentServiceDeleteAgentsProcedure,
			press:       []string{"j", "space", "a", "d", "y"},
			wantSummary: `Could not delete agent "api-refactor"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchivedAgents(t, tt.rows...)
			harness.refuse(tt.refused, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

			harness.press(append(tt.press, widenListPresses...)...)

			assert.Contains(t, readEngineLine(harness.readFrame()), tt.wantSummary)
			assert.Contains(t, readOpenMessages(harness), "no such agent")
		})
	}
}

// TestArchivedAgentsRows covers which folders and agents the archived agents kind renders.
func TestArchivedAgentsRows(t *testing.T) {
	tests := []struct {
		name     string
		rows     []proto.Message
		wantRows []string
	}{
		{
			name:     "nothing archived renders nothing",
			rows:     []proto.Message{newLiveWorkflow("w1", "backend")},
			wantRows: []string{},
		},
		{
			name:     "an archived folder emptied of its agents keeps its header",
			rows:     []proto.Message{newArchivedWorkflow("w1", "backend")},
			wantRows: []string{"backend"},
		},
		{
			name: "a live folder with nothing put away out of it gets no header",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newLiveWorkflow("w2", "frontend"),
				newArchivedAgent("a1", "auth", "w1"),
			},
			wantRows: []string{"backend", "└─ auth"},
		},
		{
			name: "an orchestrator folds onto its folder",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				&agentv1.Agent{
					Id:         "o1",
					Name:       "orchestrator",
					WorkflowId: "w1",
					Role:       agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR,
					ArchivedAt: xptr.Ptr(archivedAt),
				},
			},
			wantRows: []string{"backend"},
		},
		{
			name: "live and archived folders are rendered together by name",
			rows: []proto.Message{
				newLiveWorkflow("w2", "frontend"),
				newArchivedWorkflow("w1", "backend"),
				newArchivedWorkflow("w3", "infra"),
				newArchivedAgent("a1", "auth", "w1"),
				newArchivedAgent("a2", "ui", "w2"),
			},
			wantRows: []string{"backend", "└─ auth", "frontend", "└─ ui", "infra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchivedAgents(t, tt.rows...)

			assert.Equal(t, tt.wantRows, readListRows(harness.readFrame()))
		})
	}
}

// TestArchivedAgentsRowEmphasis covers what the archived agents' rows say in colour.
func TestArchivedAgentsRowEmphasis(t *testing.T) {
	t.Run("an archived folder is greyed and a live one is not", func(t *testing.T) {
		harness := openArchivedAgents(t,
			newLiveWorkflow("w1", "backend"),
			newArchivedAgent("a1", "auth", "w1"),
			newArchivedWorkflow("w2", "frontend"),
			newArchivedAgent("a2", "ui", "w2"),
		)

		harness.press("G")

		frame := harness.readRawFrame()
		muted := paint.Panel.Style(paint.ToneMuted).Bold(true)
		assert.Contains(t, frame, muted.Render("frontend"))
		assert.NotContains(t, frame, muted.Render("backend"))
	})
}

// TestArchivedAgentsMenuEntries covers the archive menu naming what the hovered row takes.
func TestArchivedAgentsMenuEntries(t *testing.T) {
	neverStarted := newArchivedAgent("a1", "api-refactor", "w1")
	neverStarted.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
	openOff := readOffEntry("restore and open", "o")
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
			rows:    []proto.Message{newArchivedWorkflow("w1", "backend")},
			want:    []string{"restore", "delete for good"},
			wantOff: true,
		},
		{
			name:    "on an agent that never started",
			rows:    []proto.Message{newLiveWorkflow("w1", "backend"), neverStarted},
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
			harness := openArchivedAgents(t, tt.rows...)

			harness.press(append(tt.press, "space")...)
			if tt.wantNone {
				assert.NotRegexp(t, archiveEntry, harness.readFrame())

				return
			}
			require.Regexp(t, archiveEntry, harness.readFrame())

			harness.press("a")
			frame := harness.readFrame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			if tt.wantOff {
				assert.Regexp(t, openOff, harness.readRawFrame())
			} else {
				assert.NotRegexp(t, openOff, harness.readRawFrame())
			}
		})
	}

	t.Run("an archived folder's menu holds none of its orchestrator's entries", func(t *testing.T) {
		orchestrator := newArchivedAgent("o1", "orchestrator", "w1")
		orchestrator.Role = agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR
		harness := openArchivedAgents(t, newArchivedWorkflow("w1", "backend"), orchestrator)

		harness.press("space")

		menu := strings.Join(readStage(harness.readFrame()), "\n")
		require.Regexp(t, archiveEntry, menu)
		for _, entry := range []string{"logs", "terminal", "jump ›"} {
			assert.NotContains(t, menu, entry)
		}
	})
}

// TestArchivedAgentsRename covers a name typed over an archived agents row going out as its rename.
func TestArchivedAgentsRename(t *testing.T) {
	archived := []proto.Message{
		newArchivedWorkflow("w1", "backend"),
		newArchivedAgent("a1", "api-refactor", "w1"),
		newArchivedSchedule("s2", "weekly-gone"),
	}
	live := []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newArchivedAgent("a1", "api-refactor", "w1"),
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
			press:     slices.Concat([]string{"j", "r"}, spellKeys("-2"), []string{"enter"}),
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
			want:      &agentv1.UpdateAgentRequest{Id: "a1", Name: "api-refactor-2"},
		},
		{
			name:      "a live workflow's folder",
			rows:      live,
			press:     slices.Concat([]string{"r"}, spellKeys("-2"), []string{"enter"}),
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
			harness := openArchivedAgents(t, tt.rows...)

			harness.press(tt.press...)

			assert.Equal(t, agentsScreen, harness.readScreen())
			if tt.want == nil {
				assert.NotContains(t, harness.readFrame(), "▏")
				harness.assertSent()

				return
			}
			harness.assertSentTo(tt.procedure, tt.want)
		})
	}
}

// TestArchivedAgentsDocument covers what the panel renders beside the list for the hovered row.
func TestArchivedAgentsDocument(t *testing.T) {
	stuck := newArchivedAgent("a1", "api-refactor", "w1")
	stuck.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
	stuck.FailureReason = "the worktree is dirty"
	measured := newArchivedAgent("a1", "api-refactor", "w1")
	measuredTask := newTaskOf(measured)
	measuredTask.DiffStat = &taskv1.DiffStat{Added: 200, Removed: 100}
	measuredTask.ArchivedAt = xptr.Ptr(archivedAt)
	measuredReview := newArchivedAgent("a2", "api-review", "w1")
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
			rows: []proto.Message{newLiveWorkflow("w1", "backend")},
			want: []string{"nothing archived", "until you delete them"},
		},
		{
			name:       "an agent a delete could not destroy says why",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend"), stuck},
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
			rows:       []proto.Message{newLiveWorkflow("w1", "backend"), measured, measuredTask, measuredReview},
			press:      []string{"j", "j"},
			want:       []string{"archived — kept until deleted"},
			wantAbsent: []string{"+200"},
		},
		{
			name:       "an archived task renders no diff stat",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend"), measured, measuredTask},
			press:      []string{"j"},
			want:       []string{"task-a1", "no goal set"},
			wantAbsent: []string{"+200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchivedAgents(t, tt.rows...)

			harness.press(tt.press...)

			frame := harness.readFrame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
		})
	}

	t.Run("the record says what the agents list would say of it", func(t *testing.T) {
		agent := newArchivedAgent("a1", "api-refactor", "w1")
		agent.CreatedAt = "2026-07-29T09:00:00Z"
		task := newTaskOf(agent)
		task.Branch = xptr.Ptr("agent/a1")
		task.RepositoryUrl = xptr.Ptr("https://github.com/harrisoncramer/komrad")
		task.CreatedAt = "2026-07-29T09:00:00Z"
		task.ArchivedAt = xptr.Ptr(archivedAt)
		harness := openArchivedAgents(t,
			newLiveWorkflow("w1", "backend"),
			agent,
			task,
			&pullrequestv1.PullRequest{
				Id:                "pr-42",
				RepositoryUrl:     "https://github.com/harrisoncramer/komrad",
				HeadRepositoryUrl: "https://github.com/harrisoncramer/komrad",
				HeadBranch:        "agent/a1",
				Number:            42,
				State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
				OpenedAt:          "2026-07-30T08:00:00Z",
				MergedAt:          xptr.Ptr(archivedAt),
				ClosedAt:          xptr.Ptr(archivedAt),
			},
		)

		harness.press(append([]string{"j", "pick:record"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "agent/a1", "the task's record carries its checkout")
		assert.Contains(t, frame, "its pull request is merged")
		created, archived := findRecordLine(frame, "created"), findRecordLine(frame, "archived")
		require.Positive(t, created)
		assert.Less(t, created, archived, "when it was put away comes after when it was made")
	})

	t.Run("the document follows the cursor onto the folder", func(t *testing.T) {
		workflow := newLiveWorkflow("w1", "backend")
		workflow.Context = "the shared notes"
		harness := openArchivedAgents(t, workflow, newArchivedAgent("a1", "api-refactor", "w1"))

		frame := harness.readFrame()
		assert.Contains(t, frame, "the shared notes")
		for _, row := range splitLines(frame) {
			assert.LessOrEqual(t, ansi.StringWidth(row), defaultHarnessWidth, "neither column runs past the window")
		}
	})
}

// TestArchivedAgentsSort covers ordering the archived agents by their column tabs.
func TestArchivedAgentsSort(t *testing.T) {
	unsorted := []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newArchivedAgentAt("zeta", "2026-07-30T12:00:00Z"),
		newArchivedAgentAt("mid", "2026-07-30T13:00:00Z"),
		newArchivedAgentAt("alpha", "2026-07-30T09:00:00Z"),
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
			harness := openArchivedAgents(t, unsorted...)

			harness.press(tt.press...)

			assert.Equal(t, tt.want, readListRows(harness.readFrame()))
		})
	}

	t.Run("the archived agents open under their folders, the most recently archived first", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.watch(buildSnapshot(unsorted...))

		harness.press("F", "tab", "esc")

		assert.Equal(t, []string{"backend", "├─ mid", "├─ zeta", "└─ alpha"}, readListRows(harness.readFrame()))
	})

	t.Run("the top and bottom keys walk the archived agents' own rows", func(t *testing.T) {
		harness := openArchivedAgents(t, unsorted...)

		harness.press("G")
		assert.Equal(t, "└─ alpha", readHoveredRow(harness.readFrame()))

		harness.press("g")
		assert.Equal(t, "└─ alpha", readHoveredRow(harness.readFrame()), "the first press waits for the second")

		harness.press("g")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))
	})

	t.Run("the cursor keeps its row as the archived agents are reordered", func(t *testing.T) {
		harness := openArchivedAgents(t, unsorted...)
		harness.press("j", "j", "j")
		require.Equal(t, "└─ alpha", readHoveredRow(harness.readFrame()))

		harness.press("f", "tab", "k", "esc")
		assert.Equal(t, "├─ alpha", readHoveredRow(harness.readFrame()), "name up")

		harness.press("f", "j", "esc")
		assert.Equal(t, "└─ alpha", readHoveredRow(harness.readFrame()), "name down")
	})
}

// TestArchivedAgentsWindow covers the archived agents holding the window and the keyboard.
func TestArchivedAgentsWindow(t *testing.T) {
	t.Run("entering the archived agents asks for nothing", func(t *testing.T) {
		harness := openArchivedAgents(t, buildOneArchivedAgent()...)

		assert.Contains(t, harness.readFrame(), "api-refactor")
		harness.assertSent()
	})

	t.Run("the archived agents take the window and gives it back for a running agent", func(t *testing.T) {
		harness := openArchivedAgents(t,
			newLiveWorkflow("w1", "backend"),
			&agentv1.Agent{
				Id:         "a2",
				Name:       "auth",
				WorkflowId: "w1",
				PaneId:     "%9",
				Alive:      xptr.Ptr(true),
				State:      agentv1.AgentState_AGENT_STATE_IDLE,
			},
			newArchivedAgent("a1", "api-refactor", "w1"),
		)
		require.True(t, harness.panel.IsZoomed(), "the archived agents render both columns themselves")

		harness.press("F", "tab", "esc", "j")
		harness.fireTimers()

		assert.False(t, harness.panel.IsZoomed(), "leaving the archived agents for a running agent gives the other panes their share back")
	})

	t.Run("restoring and opening an agent hands the window to the pane it opens", func(t *testing.T) {
		harness := openArchivedAgents(t, buildOneArchivedAgent()...)
		require.True(t, harness.panel.IsZoomed())
		answerResume(harness)

		harness.press("j", "space", "a", "o")
		harness.watch(buildPuts(newRestoredAgent()))

		assert.False(t, harness.panel.IsZoomed(), "the pane it made is not left under the archived agents' own zoom")
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("a short window keeps the list beside the document", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{
			width:  120,
			height: 10,
		})
		harness.watch(buildSnapshot(buildOneArchivedAgent()...))

		harness.press("F", "tab", "esc", "j", "?")

		frame := harness.readFrame()
		assert.Equal(t, []string{"backend", "└─ api-refactor"}, readListRows(frame), "the list keeps its column")
		assert.Contains(t, frame, "archived — kept until deleted", "beside the hovered row's document")
		assert.Len(t, splitLines(frame), 10, "the view is exactly the pane")
		assert.Contains(t, frame, "show help", "the legend gave up its expansion for the rows")
		assert.NotContains(t, frame, "hide help")
	})

	t.Run("the document column stands as tall as the window", func(t *testing.T) {
		workflow := newLiveWorkflow("w1", "backend")
		workflow.Context = strings.Repeat("a line of the document. ", 60)

		tests := []struct {
			name   string
			height int
		}{
			{
				name:   "a short window",
				height: 12,
			},
			{
				name:   "a middling window",
				height: 16,
			},
			{
				name:   "a tall window",
				height: 24,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				harness := newPanelHarness(t, panelHarnessParams{height: tt.height})
				harness.watch(buildSnapshot(workflow, newArchivedAgent("a1", "api-refactor", "w1")))

				harness.press("F", "tab", "esc")

				assert.Len(t, splitLines(harness.readFrame()), tt.height)
			})
		}
	})
}

// TestArchivedAgentsSelection covers choosing several archived agents and acting on them at once.
func TestArchivedAgentsSelection(t *testing.T) {
	rows := []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newArchivedAgentAt("auth", "2026-07-30T12:00:00Z"),
		newArchivedAgentAt("api", "2026-07-30T11:00:00Z"),
		newArchivedAgentAt("db", "2026-07-30T10:00:00Z"),
	}

	tests := []struct {
		name     string
		press    []string
		wantSent []request
	}{
		{
			name:  "a header takes its whole folder",
			press: []string{"v", "space", "a", "r"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"auth", "api", "db"}},
			}},
		},
		{
			name:  "the range key reaches back to the nearest selection",
			press: []string{"j", "v", "j", "j", "V", "space", "a", "r"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"auth", "api", "db"}},
			}},
		},
		{
			name:  "restore takes the whole selection rather than the hovered row",
			press: []string{"j", "v", "j", "v", "j", "space", "a", "r"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"auth", "api"}},
			}},
		},
		{
			name:  "delete opens its prompt over the archived agents on the whole selection",
			press: []string{"j", "v", "j", "v", "j", "space", "a", "d", "y"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
				msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"auth", "api"}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchivedAgents(t, rows...)

			harness.press(tt.press...)

			assert.Equal(t, agentsScreen, harness.readScreen())
			harness.assertSent(tt.wantSent...)
		})
	}

	t.Run("a folder restore stands down while a selection is up", func(t *testing.T) {
		harness := openArchivedAgents(t, newArchivedWorkflow("w1", "backend"), newArchivedAgent("a1", "auth", "w1"))

		harness.press("j", "v", "g", "g", "space", "a", "r")

		harness.assertSent(request{
			procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
			msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
		})
	})

	t.Run("the selection outlives a refused restore", func(t *testing.T) {
		harness := openArchivedAgents(t, rows...)
		harness.refuse(agentv1connect.AgentServiceRestoreAgentsProcedure, connect.NewError(connect.CodeUnavailable, errors.New("down")))

		harness.press("j", "v", "space", "a", "r")

		assert.Contains(t, readLegend(harness.readFrame()), "1 selected")
	})

	t.Run("escape clears the selection before it leaves the archived agents", func(t *testing.T) {
		harness := openArchivedAgents(t, rows...)
		harness.press("j", "v")

		harness.press("esc")

		frame := harness.readFrame()
		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.NotContains(t, readLegend(frame), "selected")

		harness.press("F", "tab", "esc")
		assert.Equal(t, agentsScreen, harness.readScreen())
	})

	t.Run("an agent gone from the archived agents drops out of the selection", func(t *testing.T) {
		harness := openArchivedAgents(t, rows...)
		harness.press("j", "v", "G", "v")

		harness.watch(buildSnapshot(rows[0], rows[3]))
		harness.press("space", "a", "r")

		harness.assertSentTo(agentv1connect.AgentServiceRestoreAgentsProcedure, &agentv1.RestoreAgentsRequest{Ids: []string{"db"}})
	})

	t.Run("selecting among the archived agents leaves the agents tree alone", func(t *testing.T) {
		harness := openArchivedAgents(t, append(slices.Clone(rows), newAgent("a9", "live", "w1"))...)
		harness.press("j", "v", "esc", "F", "tab", "esc")

		assert.NotContains(t, readLegend(harness.readFrame()), "selected")
	})
}

// TestArchivedAgentsKinds covers the row of kinds switching the agents view between its live and archived agents.
func TestArchivedAgentsKinds(t *testing.T) {
	rows := []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newAgent("a9", "auth-live", "w1"),
		newArchivedAgent("a1", "api-refactor", "w1"),
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
			name:       "the row of kinds again goes back to the live agents",
			press:      []string{"F", "tab", "tab", "esc"},
			want:       "auth-live",
			wantAbsent: "api-refactor",
		},
		{
			name:       "shift-tab on the row of kinds wraps to the archived agents",
			press:      []string{"F", "shift+tab", "esc"},
			want:       "api-refactor",
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
			harness := openAgents(t, rows...)

			harness.press(tt.press...)
			harness.press(widenListPresses...)

			frame := ansi.Strip(harness.readFrame())
			assert.Equal(t, agentsScreen, harness.readScreen())
			assert.Contains(t, frame, tt.want)
			assert.NotContains(t, frame, tt.wantAbsent)
		})
	}

	t.Run("the row of kinds refuses the keys while agents are selected", func(t *testing.T) {
		harness := openAgents(t, rows...)

		harness.press(append(widenListPresses, "j", "v", "F", "?")...)
		frame := harness.readFrame()
		assert.NotEmpty(t, readHoveredRow(frame), "the list keeps the keys")
		assert.NotContains(t, readLegend(frame), "F kind")
	})

	t.Run("the row of kinds holds the keys until escape", func(t *testing.T) {
		harness := openAgents(t, rows...)

		harness.press(append(widenListPresses, "F")...)
		frame := harness.readFrame()
		legend := readLegend(frame)
		assert.Contains(t, legend, "tab/S-tab switch")
		assert.Contains(t, legend, "esc back to the list")
		assert.Empty(t, readHoveredRow(frame), "the row's arrow stands down")

		harness.press("esc")
		assert.NotEmpty(t, readHoveredRow(harness.readFrame()), "and the list has the keys again")
	})

	t.Run("each kind comes back on the row it was left on", func(t *testing.T) {
		harness := openAgents(t,
			newLiveWorkflow("w1", "backend"),
			newAgent("a8", "auth-live", "w1"),
			newAgent("a9", "billing-live", "w1"),
			newArchivedAgent("a1", "api-refactor", "w1"),
			newArchivedAgent("a2", "billing", "w1"),
		)
		harness.press(append(widenListPresses, "j")...)
		live := readHoveredRow(harness.readFrame())
		harness.press("F", "tab", "esc", "j")
		archived := readHoveredRow(harness.readFrame())

		harness.press("F", "tab", "esc")
		assert.Equal(t, live, readHoveredRow(harness.readFrame()))

		harness.press("F", "tab", "esc")
		assert.Equal(t, archived, readHoveredRow(harness.readFrame()))
	})
}

// TestArchivedAgentsLastScreen covers the saved screen rendered under an archived agent.
func TestArchivedAgentsLastScreen(t *testing.T) {
	t.Run("the saved screen is asked for once the cursor rests and rendered under the document", func(t *testing.T) {
		harness := openArchivedAgents(t, buildOneArchivedAgent()...)
		answerLastScreens(harness, map[string]string{"a1": "❯ ship the refactor\n⏺ the refactor is merged"})

		harness.press("j")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceGetLastScreenProcedure), "nothing is asked while the cursor moves")

		harness.fireTimers()

		requireLastScreenFor(t, harness, "a1")
		frame := harness.readFrame()
		for _, want := range []string{"archived · press space a o to restore", "Restore brings it back", "saved screen", "80x24", "❯ ship the refactor", "⏺ the refactor is merged"} {
			assert.Contains(t, frame, want)
		}
	})

	t.Run("a saved screen taller than the room left is fitted under the document and its banner", func(t *testing.T) {
		harness := openArchivedAgents(t, buildOneArchivedAgent()...)
		harness.answer(agentv1connect.AgentServiceGetLastScreenProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.GetLastScreenResponse{
				Frame:      "❯ the first ask\n" + strings.Repeat("working through it\n", 60) + "⏺ the refactor is merged",
				Width:      80,
				Height:     62,
				CapturedAt: timestamppb.New(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)),
			}), nil
		})

		harness.press("j")
		harness.fireTimers()

		frame := harness.readFrame()
		for _, want := range []string{"Restore brings it back", "saved screen", "⏺ the refactor is merged"} {
			assert.Contains(t, frame, want)
		}
		assert.NotContains(t, frame, "the first ask")
	})

	t.Run("an agent with no saved screen says so", func(t *testing.T) {
		harness := openArchivedAgents(t, buildOneArchivedAgent()...)
		answerLastScreens(harness, nil)

		harness.press("j")
		harness.fireTimers()

		assert.Contains(t, harness.readFrame(), "no saved screen")
	})

	t.Run("a refused read says why", func(t *testing.T) {
		harness := openArchivedAgents(t, buildOneArchivedAgent()...)
		harness.answer(agentv1connect.AgentServiceGetLastScreenProcedure, refuseWith(connect.CodeInternal, "screens exploded"))

		harness.press("j")
		harness.fireTimers()

		frame := harness.readFrame()
		assert.Contains(t, frame, "could not read the saved screen: screens exploded")
		assert.Contains(t, frame, "Restore brings it back")
	})

	t.Run("a folder has no saved screen", func(t *testing.T) {
		harness := openArchivedAgents(t, buildOneArchivedAgent()...)
		harness.fireTimers()

		assert.NotContains(t, harness.readFrame(), "saved screen")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceGetLastScreenProcedure))
	})

	t.Run("an answer for an agent the cursor has left is not rendered", func(t *testing.T) {
		harness := openArchivedAgents(t,
			newLiveWorkflow("w1", "backend"),
			newArchivedAgent("a1", "api-refactor", "w1"),
			newArchivedAgent("a2", "billing", "w1"),
		)
		answerLastScreens(harness, map[string]string{
			"a1": "words from api-refactor",
			"a2": "words from billing",
		})
		harness.hold(agentv1connect.AgentServiceGetLastScreenProcedure)

		harness.press("j")
		harness.fireTimers()
		harness.press("j")
		harness.release(agentv1connect.AgentServiceGetLastScreenProcedure)

		frame := harness.readFrame()
		assert.NotContains(t, frame, "words from api-refactor")
		assert.NotContains(t, frame, "words from billing")
		assert.Contains(t, frame, "reading the saved screen")
	})

	t.Run("clearing a search that moved the cursor asks for the agent it lands back on", func(t *testing.T) {
		harness := openArchivedAgents(t,
			newLiveWorkflow("w1", "backend"),
			newArchivedAgent("a1", "api-refactor", "w1"),
			newArchivedAgent("a2", "billing", "w1"),
		)
		answerLastScreens(harness, map[string]string{
			"a1": "words from api-refactor",
			"a2": "words from billing",
		})
		harness.press("j")
		harness.fireTimers()

		harness.press("/", "a", "p", "i", "enter")
		harness.fireTimers()
		require.Contains(t, harness.readFrame(), "words from api-refactor")

		harness.press("esc")
		harness.fireTimers()

		assert.Contains(t, harness.readFrame(), "words from billing")
	})

	t.Run("a listing that moves the cursor onto another agent asks for it", func(t *testing.T) {
		harness := openArchivedAgents(t,
			newLiveWorkflow("w1", "backend"),
			newArchivedAgent("a1", "api-refactor", "w1"),
			newArchivedAgent("a2", "billing", "w1"),
		)
		answerLastScreens(harness, nil)
		harness.press("j")
		harness.fireTimers()

		requireLastScreenFor(t, harness, "a2")

		restored := newArchivedAgent("a2", "billing", "w1")
		restored.ArchivedAt = nil
		harness.watch(buildPuts(restored))

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
			harness := openArchivedAgents(t,
				newLiveWorkflow("w1", "backend"),
				newArchivedAgent("a1", "api-refactor", "w1"),
				newArchivedAgent("a2", "billing", "w1"),
			)
			answerLastScreens(harness, map[string]string{"a1": screen, "a2": screen})
			harness.press("j")
			harness.fireTimers()

			harness.press(tt.presses...)

			frame := harness.readFrame()
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantNot {
				assert.NotContains(t, frame, absent)
			}
		})
	}

	t.Run("the saved screen takes the keys while the live list stands on a stopped agent", func(t *testing.T) {
		stopped := newDormant("sleepy")
		stopped.Id = "d1"
		harness := newPanelHarness(t, panelHarnessParams{})
		answerLastScreens(harness, map[string]string{"a1": screen, "a2": screen})
		harness.watch(buildSnapshot(
			newLiveWorkflow("w1", "backend"),
			stopped,
			newArchivedAgent("a1", "api-refactor", "w1"),
			newArchivedAgent("a2", "billing", "w1"),
		))
		harness.press("F", "tab", "esc", "j")
		harness.fireTimers()

		harness.press("pick:block", "G")

		assert.Contains(t, harness.readFrame(), "line number 39")
	})
}

func openArchivedAgents(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{})
	harness.press("F", "tab", "esc")
	harness.watch(buildSnapshot(rows...))

	return harness
}

func buildOneArchivedAgent() []proto.Message {
	return []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newArchivedAgent("a1", "api-refactor", "w1"),
	}
}

func newLiveWorkflow(id, name string) *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:   id,
		Name: name,
	}
}

func newArchivedWorkflow(id, name string) *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:         id,
		Name:       name,
		ArchivedAt: xptr.Ptr(archivedAt),
	}
}

func newArchivedAgent(id, name, workflowID string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         id,
		Name:       name,
		WorkflowId: workflowID,
		State:      agentv1.AgentState_AGENT_STATE_IDLE,
		ArchivedAt: xptr.Ptr(archivedAt),
	}
}

func newRestoredAgent() *agentv1.Agent {
	agent := newArchivedAgent("a1", "api-refactor", "w1")
	agent.ArchivedAt = nil
	agent.PaneId = "%9"
	agent.Alive = xptr.Ptr(true)

	return agent
}

func newArchivedAgentAt(name, at string) *agentv1.Agent {
	agent := newArchivedAgent(name, name, "w1")
	agent.ArchivedAt = xptr.Ptr(at)

	return agent
}

// answerLastScreens answers each agent's saved screen at 80x24, and not found for any other.
func answerLastScreens(harness *panelHarness, screens map[string]string) {
	harness.answer(agentv1connect.AgentServiceGetLastScreenProcedure, func(req connect.AnyRequest) (connect.AnyResponse, error) {
		read, _ := req.Any().(*agentv1.GetLastScreenRequest)

		frame, ok := screens[read.GetAgentId()]
		if !ok {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("no saved screen"))
		}

		return connect.NewResponse(&agentv1.GetLastScreenResponse{
			Frame:      frame,
			Width:      80,
			Height:     24,
			CapturedAt: timestamppb.New(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)),
		}), nil
	})
}

func requireLastScreenFor(t *testing.T, harness *panelHarness, agentIDs ...string) {
	t.Helper()

	sent := xslice.Map(harness.readSent(agentv1connect.AgentServiceGetLastScreenProcedure), func(msg proto.Message) string {
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
	emptied := slices.DeleteFunc(buildArchivedTasks(), func(row proto.Message) bool {
		agent, ok := row.(*agentv1.Agent)

		return ok && agent.GetId() == "a5"
	})
	onAPIWork := []string{"j", "j", "j", "j"}

	tests := []struct {
		name      string
		rows      []proto.Message
		press     []string
		wantFrame string
		wantSent  []request
	}{
		{
			name:  "restore on a task restores its writer alone",
			rows:  buildArchivedTasks(),
			press: append(onAPIWork, "space", "a", "r"),
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:  "restore and open on a task restores its writer and opens it",
			rows:  buildArchivedTasks(),
			press: append(onAPIWork, "space", "a", "o"),
			wantSent: []request{
				{
					procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
					msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
				},
				{
					procedure: agentv1connect.AgentServiceResumeAgentProcedure,
					msg:       &agentv1.ResumeAgentRequest{Id: "a1"},
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
			name:  "delete on a task deletes the task",
			rows:  buildArchivedTasks(),
			press: append(onAPIWork, "space", "a", "d", "y"),
			wantSent: []request{{
				procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
				msg:       &taskv1.DeleteTasksRequest{Tasks: []string{"t1"}},
			}},
		},
		{
			name:      "the delete prompt on a task says the task goes",
			rows:      buildArchivedTasks(),
			press:     append(onAPIWork, "space", "a", "d"),
			wantFrame: `It also deletes task "api-work"`,
		},
		{
			name:  "restore on a live task's header restores only its archived agents",
			rows:  buildArchivedTasks(),
			press: []string{"j", "space", "a", "r"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a4"}},
			}},
		},
		{
			name:  "delete on a live task's header deletes only its archived agents",
			rows:  buildArchivedTasks(),
			press: []string{"j", "space", "a", "d", "y"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
				msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"a4"}},
			}},
		},
		{
			name:  "delete on a live task with no live agent left still spares the task",
			rows:  emptied,
			press: []string{"j", "space", "a", "d", "y"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
				msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"a4"}},
			}},
		},
		{
			name:  "the range key takes the tasks between",
			rows:  buildArchivedTasks(),
			press: []string{"j", "j", "j", "v", "j", "V", "space", "a", "d", "y"},
			wantSent: []request{{
				procedure: taskv1connect.TaskServiceDeleteTasksProcedure,
				msg:       &taskv1.DeleteTasksRequest{Tasks: []string{"t2", "t1"}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchivedAgents(t, tt.rows...)
			answerResume(harness)

			harness.press(tt.press...)

			if tt.wantFrame != "" {
				assert.Contains(t, harness.readFrame(), tt.wantFrame)
			}
			harness.assertSent(tt.wantSent...)
		})
	}

	t.Run("a task's menu leaves out what only a live task takes", func(t *testing.T) {
		harness := openArchivedAgents(t, buildArchivedTasks()...)

		harness.press(append(onAPIWork, "space")...)

		frame := harness.readFrame()
		require.Regexp(t, `archive ›\s+a`, frame)
		assert.NotContains(t, frame, "hide")
		assert.NotContains(t, frame, "edit task")
	})
}

// buildArchivedTasks is two archived tasks and a live one with an agent archived out of it.
func buildArchivedTasks() []proto.Message {
	refactor := newArchivedAgent("a1", "api-refactor", "w1")
	review := newArchivedAgent("a2", "api-review", "w1")
	tune := newArchivedAgent("a3", "db-tune", "w1")
	auth := newArchivedAgent("a4", "auth", "w1")
	authLive := newAgent("a5", "auth-live", "w1")
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
		newLiveWorkflow("w1", "backend"),
		refactor, review, tune, auth, authLive,
		&taskv1.Task{
			Id:         "t1",
			WorkflowId: "w1",
			Slug:       "api-work",
			ArchivedAt: xptr.Ptr(archivedAt),
		},
		&taskv1.Task{
			Id:         "t2",
			WorkflowId: "w1",
			Slug:       "db-work",
			ArchivedAt: xptr.Ptr(archivedAt),
		},
		&taskv1.Task{
			Id:         "t3",
			WorkflowId: "w1",
			Slug:       "auth-work",
		},
	}
}
