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
	schedulev1 "komrad/proto/gen/schedule/v1"
	"komrad/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	archivedAt    = "2026-07-30T09:00:00Z"
	archiveScreen = "the archive"
	agentsScreen  = "the agents list"
	deletePrompt  = "Permanently delete"
)

var widenListPresses = slices.Repeat([]string{"}"}, 10)

// TestArchiveMenu covers what each entry of the archive menu does on the row under the cursor.
func TestArchiveMenu(t *testing.T) {
	neverStarted := newArchivedAgent("a1", "api-refactor", "w1")
	neverStarted.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE

	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		wantScreen string
		wantFrame  string
		wantSent   []request
	}{
		{
			name:       "restore on an empty archive asks for no write",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend")},
			press:      []string{"space", "a", "r"},
			wantScreen: archiveScreen,
		},
		{
			name:       "delete on an empty archive opens no prompt",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend")},
			press:      []string{"space", "a", "d"},
			wantScreen: archiveScreen,
		},
		{
			name:       "restore on a live folder's header asks for no write",
			rows:       buildOneArchivedAgent(),
			press:      []string{"space", "a", "r"},
			wantScreen: archiveScreen,
		},
		{
			name:       "delete on a live folder's header opens no prompt",
			rows:       buildOneArchivedAgent(),
			press:      []string{"space", "a", "d"},
			wantScreen: archiveScreen,
		},
		{
			name:       "restore and open passes over a folder",
			rows:       buildOneArchivedAgent(),
			press:      []string{"space", "a", "o"},
			wantScreen: archiveScreen,
		},
		{
			name:       "restore and open passes over an agent that never started",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend"), neverStarted},
			press:      []string{"j", "space", "a", "o", "ctrl+g"},
			wantScreen: archiveScreen,
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
			wantScreen: archiveScreen,
			wantFrame:  "one agent's conversation back in a pane",
		},
		{
			name:       "back opens the view switcher",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "esc", "tab", "tab"},
			wantScreen: agentsScreen,
		},
		{
			name:       "restore puts the hovered agent back and stays on the archive",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "space", "a", "r"},
			wantScreen: archiveScreen,
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:       "restore and open puts the agent back and opens it",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "space", "a", "o"},
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
			wantScreen: archiveScreen,
			wantSent: []request{{
				procedure: workflowv1connect.WorkflowServiceRestoreWorkflowProcedure,
				msg:       &workflowv1.RestoreWorkflowRequest{Id: "w1"},
			}},
		},
		{
			name:       "delete on an agent opens the confirm prompt rather than destroying",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "space", "a", "d"},
			wantScreen: archiveScreen,
			wantFrame:  `Permanently delete agent "api-refactor"?`,
		},
		{
			name:       "delete on an archived folder opens the confirm prompt",
			rows:       []proto.Message{newArchivedWorkflow("w1", "backend")},
			press:      []string{"space", "a", "d"},
			wantScreen: archiveScreen,
			wantFrame:  `Permanently delete workflow "backend"?`,
		},
		{
			name:       "delete on an archived folder says the agent inside it goes too",
			rows:       []proto.Message{newArchivedWorkflow("w1", "backend"), newArchivedAgent("a1", "api-refactor", "w1")},
			press:      []string{"space", "a", "d"},
			wantScreen: archiveScreen,
			wantFrame:  "The one agent still archived inside it goes with it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openGroupedArchive(t, tt.rows...)

			harness.press(tt.press...)

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

// TestArchivePrompts covers answering the delete prompts.
func TestArchivePrompts(t *testing.T) {
	tests := []struct {
		name     string
		rows     []proto.Message
		press    []string
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
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
				msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:  "confirming the workflow delete destroys the workflow it was opened on",
			rows:  []proto.Message{newArchivedWorkflow("w1", "backend")},
			press: []string{"space", "a", "d", "y"},
			wantSent: []request{{
				procedure: workflowv1connect.WorkflowServiceDeleteWorkflowProcedure,
				msg:       &workflowv1.DeleteWorkflowRequest{Id: "w1"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openGroupedArchive(t, tt.rows...)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Equal(t, archiveScreen, harness.readScreen(), "the prompt returns to the archive it was opened from")
			assert.NotContains(t, frame, deletePrompt, "the prompt is down")
			harness.assertSent(tt.wantSent...)
		})
	}
}

// TestArchiveReportsRefusedWrites covers the banner a refused write leaves.
func TestArchiveReportsRefusedWrites(t *testing.T) {
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
			harness := openGroupedArchive(t, tt.rows...)
			harness.refuse(tt.refused, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

			harness.press(append(tt.press, widenListPresses...)...)

			assert.Contains(t, readEngineLine(harness.readFrame()), tt.wantSummary)
			assert.Contains(t, readOpenMessages(harness), "no such agent")
		})
	}
}

// TestArchiveRows covers which folders and agents the archive lists.
func TestArchiveRows(t *testing.T) {
	tests := []struct {
		name     string
		rows     []proto.Message
		wantRows []string
	}{
		{
			name: "nothing archived draws nothing",
			rows: []proto.Message{newLiveWorkflow("w1", "backend")},
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
			wantRows: []string{"backend", "  auth"},
		},
		{
			name: "an orchestrator is a row of its own rather than folded onto its folder",
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
			wantRows: []string{"backend", "  orchestrator"},
		},
		{
			name: "live and archived folders are drawn together by name",
			rows: []proto.Message{
				newLiveWorkflow("w2", "frontend"),
				newArchivedWorkflow("w1", "backend"),
				newArchivedWorkflow("w3", "infra"),
				newArchivedAgent("a1", "auth", "w1"),
				newArchivedAgent("a2", "ui", "w2"),
			},
			wantRows: []string{"backend", "  auth", "frontend", "  ui", "infra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openGroupedArchive(t, tt.rows...)

			assert.Equal(t, tt.wantRows, readListRows(harness.readFrame()))
		})
	}
}

// TestArchiveRowEmphasis covers what the archive's rows say in colour and in their bar.
func TestArchiveRowEmphasis(t *testing.T) {
	t.Run("an archived folder is greyed and a live one is not", func(t *testing.T) {
		harness := openGroupedArchive(t,
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

	t.Run("an agent a delete could not destroy draws a busy bar", func(t *testing.T) {
		stuck := newArchivedAgent("a2", "stuck", "w1")
		stuck.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
		harness := openGroupedArchive(t, newLiveWorkflow("w1", "backend"), newArchivedAgent("a1", "auth", "w1"), stuck)

		frame := harness.readFrame()

		assert.Contains(t, findLine(splitLines(frame), "auth"), "│   auth")
		assert.Contains(t, findLine(splitLines(frame), "stuck"), "┃   stuck")
	})
}

// TestArchiveMenuEntries covers the archive menu naming what the hovered row takes.
func TestArchiveMenuEntries(t *testing.T) {
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
			harness := openGroupedArchive(t, tt.rows...)

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
}

// TestArchiveRename covers a name typed over an archive row going out as its rename.
func TestArchiveRename(t *testing.T) {
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
			want:      &workflowv1.UpdateWorkflowRequest{Id: "w1", Name: "backend-2", Worktree: &workflowv1.WorktreeConfig{}},
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
			harness := openGroupedArchive(t, tt.rows...)

			harness.press(tt.press...)

			assert.Equal(t, archiveScreen, harness.readScreen())
			if tt.want == nil {
				assert.NotContains(t, harness.readFrame(), "▏")
				harness.assertSent()

				return
			}
			harness.assertSentTo(tt.procedure, tt.want)
		})
	}
}

// TestArchiveDocument covers what is drawn beside the list for the hovered row.
func TestArchiveDocument(t *testing.T) {
	stuck := newArchivedAgent("a1", "api-refactor", "w1")
	stuck.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
	stuck.FailureReason = "the worktree is dirty"
	measured := newArchivedAgent("a1", "api-refactor", "w1")
	measured.DiffStat = &agentv1.DiffStat{Added: 200, Removed: 100}

	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		want       []string
		wantAbsent []string
	}{
		{
			name: "an empty archive says what the archive is for",
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
			name:       "an archived agent draws no diff stat",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend"), measured},
			press:      []string{"j"},
			want:       []string{"archived — kept until deleted"},
			wantAbsent: []string{"+200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openGroupedArchive(t, tt.rows...)

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
		agent.Branch = xptr.Ptr("agent/a1")
		agent.RepositoryUrl = xptr.Ptr("https://github.com/harrisoncramer/komrad")
		harness := openGroupedArchive(t,
			newLiveWorkflow("w1", "backend"),
			agent,
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
			&agentv1.WatchedReview{
				Id:           "v1",
				AgentId:      "a1",
				RoundId:      "a1-round-1",
				RoundOrdinal: 1,
				ReleasedAt:   archivedAt,
			},
			&agentv1.ReviewThread{
				Id:       "t1",
				AgentId:  "a1",
				ReviewId: "v1",
			},
		)

		harness.press(append([]string{"j", "pick:record"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "agent/a1")
		assert.Contains(t, frame, "#42 merged")
		assert.Contains(t, frame, "your comments: 1 open, 0 resolved")
		created, archived := findRecordLine(frame, "created"), findRecordLine(frame, "archived")
		require.Positive(t, created)
		assert.Less(t, created, archived, "when it was put away comes after when it was made")
	})

	t.Run("the document follows the cursor onto the folder", func(t *testing.T) {
		workflow := newLiveWorkflow("w1", "backend")
		workflow.Context = "the shared notes"
		harness := openGroupedArchive(t, workflow, newArchivedAgent("a1", "api-refactor", "w1"))

		frame := harness.readFrame()
		assert.Contains(t, frame, "the shared notes")
		for _, row := range splitLines(frame) {
			assert.LessOrEqual(t, ansi.StringWidth(row), defaultHarnessWidth, "neither column runs past the window")
		}
	})
}

// TestArchiveSort covers ordering the archive by its column tabs.
func TestArchiveSort(t *testing.T) {
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
			want: []string{"backend", "  mid", "  zeta", "  alpha"},
		},
		{
			name:  "archived up is the oldest first",
			press: []string{"f", "k"},
			want:  []string{"backend", "  alpha", "  zeta", "  mid"},
		},
		{
			name:  "name up takes every row",
			press: []string{"f", "tab", "k"},
			want:  []string{"backend", "  alpha", "  mid", "  zeta"},
		},
		{
			name:  "name down",
			press: []string{"f", "tab", "k", "j"},
			want:  []string{"backend", "  zeta", "  mid", "  alpha"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openGroupedArchive(t, unsorted...)

			harness.press(tt.press...)

			assert.Equal(t, tt.want, readListRows(harness.readFrame()))
		})
	}

	t.Run("the archive opens flat, the most recently archived first", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{})
		harness.watch(buildSnapshot(unsorted...))

		harness.press("shift+tab", "shift+tab")

		assert.Equal(t, []string{"mid", "zeta", "alpha"}, readListRows(harness.readFrame()))
	})

	t.Run("the top and bottom keys walk the archive's own rows", func(t *testing.T) {
		harness := openGroupedArchive(t, unsorted...)

		harness.press("G")
		assert.Equal(t, "  alpha", readHoveredRow(harness.readFrame()))

		harness.press("g")
		assert.Equal(t, "  alpha", readHoveredRow(harness.readFrame()), "the first press waits for the second")

		harness.press("g")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))
	})

	t.Run("the cursor keeps its row as the archive is reordered", func(t *testing.T) {
		harness := openGroupedArchive(t, unsorted...)
		harness.press("j", "j", "j")
		require.Equal(t, "  alpha", readHoveredRow(harness.readFrame()))

		harness.press("f", "tab", "k", "esc")
		assert.Equal(t, "  alpha", readHoveredRow(harness.readFrame()), "name up")

		harness.press("f", "j", "esc")
		assert.Equal(t, "  alpha", readHoveredRow(harness.readFrame()), "name down")
	})
}

// TestArchiveWindow covers the archive holding the window and the keyboard.
func TestArchiveWindow(t *testing.T) {
	t.Run("entering the archive asks for nothing", func(t *testing.T) {
		harness := openGroupedArchive(t, buildOneArchivedAgent()...)

		assert.Contains(t, harness.readFrame(), "api-refactor")
		harness.assertSent()
	})

	t.Run("the archive takes the window and gives it back for a running agent", func(t *testing.T) {
		harness := openGroupedArchive(t,
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
		require.True(t, harness.panel.IsZoomed(), "the archive draws both columns itself")

		harness.press("esc", "tab", "tab")

		assert.False(t, harness.panel.IsZoomed(), "leaving the archive for a running agent gives the other panes their share back")
	})

	t.Run("restoring and opening an agent hands the window to the pane it opens", func(t *testing.T) {
		harness := openGroupedArchive(t, buildOneArchivedAgent()...)
		require.True(t, harness.panel.IsZoomed())
		answerResume(harness)

		harness.press("j", "space", "a", "o")

		assert.False(t, harness.panel.IsZoomed(), "the pane it made is not left under the archive's own zoom")
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("a short window keeps the list beside the document", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{
			width:  120,
			height: 10,
		})
		harness.watch(buildSnapshot(buildOneArchivedAgent()...))

		harness.press("shift+tab", "shift+tab", "?")

		frame := harness.readFrame()
		assert.Equal(t, []string{"api-refactor"}, readListRows(frame), "the list keeps its column")
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

				harness.press("shift+tab", "shift+tab")

				assert.Len(t, splitLines(harness.readFrame()), tt.height)
			})
		}
	})
}

// TestArchiveSelection covers choosing several archived agents and acting on them at once.
func TestArchiveSelection(t *testing.T) {
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
			name:  "delete opens its prompt over the archive on the whole selection",
			press: []string{"j", "v", "j", "v", "j", "space", "a", "d", "y"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
				msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"auth", "api"}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openGroupedArchive(t, rows...)

			harness.press(tt.press...)

			assert.Equal(t, archiveScreen, harness.readScreen())
			harness.assertSent(tt.wantSent...)
		})
	}

	t.Run("a folder restore stands down while a selection is up", func(t *testing.T) {
		harness := openGroupedArchive(t, newArchivedWorkflow("w1", "backend"), newArchivedAgent("a1", "auth", "w1"))

		harness.press("j", "v", "g", "g", "space", "a", "r")

		harness.assertSent(request{
			procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
			msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
		})
	})

	t.Run("the selection outlives a refused restore", func(t *testing.T) {
		harness := openGroupedArchive(t, rows...)
		harness.refuse(agentv1connect.AgentServiceRestoreAgentsProcedure, connect.NewError(connect.CodeUnavailable, errors.New("down")))

		harness.press("j", "v", "space", "a", "r")

		assert.Contains(t, readLegend(harness.readFrame()), "1 selected")
	})

	t.Run("escape clears the selection before it leaves the archive", func(t *testing.T) {
		harness := openGroupedArchive(t, rows...)
		harness.press("j", "v")

		harness.press("esc")

		frame := harness.readFrame()
		assert.Equal(t, archiveScreen, harness.readScreen())
		assert.NotContains(t, readLegend(frame), "selected")

		harness.press("esc", "tab", "tab")
		assert.Equal(t, agentsScreen, harness.readScreen())
	})

	t.Run("an agent gone from the archive drops out of the selection", func(t *testing.T) {
		harness := openGroupedArchive(t, rows...)
		harness.press("j", "v", "G", "v")

		harness.watch(buildSnapshot(rows[0], rows[3]))
		harness.press("space", "a", "r")

		harness.assertSentTo(agentv1connect.AgentServiceRestoreAgentsProcedure, &agentv1.RestoreAgentsRequest{Ids: []string{"db"}})
	})

	t.Run("selecting in the archive leaves the agents tree alone", func(t *testing.T) {
		harness := openGroupedArchive(t, append(slices.Clone(rows), newAgent("a9", "live", "w1"))...)
		harness.press("j", "v", "esc", "esc", "tab", "tab")

		assert.NotContains(t, readLegend(harness.readFrame()), "selected")
	})
}

// TestArchiveSchedules covers the archive switched to its archived schedules.
func TestArchiveSchedules(t *testing.T) {
	rows := []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newArchivedAgent("a1", "api-refactor", "w1"),
		newSchedule("s1", "nightly-live"),
		newArchivedSchedule("s2", "weekly-gone"),
	}

	tests := []struct {
		name       string
		press      []string
		want       []string
		wantAbsent []string
		wantSent   []request
	}{
		{
			name:       "the row of kinds lists the archived schedules and none of the live ones",
			press:      []string{"F", "tab", "esc"},
			want:       []string{"weekly-gone", "archived — kept until deleted"},
			wantAbsent: []string{"nightly-live", "api-refactor", "r rename"},
		},
		{
			name:       "the row of kinds again goes back to the agents",
			press:      []string{"F", "tab", "tab", "esc"},
			want:       []string{"api-refactor"},
			wantAbsent: []string{"weekly-gone"},
		},
		{
			name:  "restore puts the hovered schedule back",
			press: []string{"F", "tab", "esc", "space", "a", "r"},
			want:  []string{"weekly-gone"},
			wantSent: []request{{
				procedure: schedulev1connect.ScheduleServiceRestoreScheduleProcedure,
				msg:       &schedulev1.RestoreScheduleRequest{Id: "s2"},
			}},
		},
		{
			name:  "delete asks first",
			press: []string{"F", "tab", "esc", "space", "a", "d"},
			want:  []string{`Permanently delete schedule "weekly-gone"?`},
		},
		{
			name:       "declining the delete destroys nothing",
			press:      []string{"F", "tab", "esc", "space", "a", "d", "n"},
			wantAbsent: []string{deletePrompt},
		},
		{
			name:       "confirming the delete destroys the schedule",
			press:      []string{"F", "tab", "esc", "space", "a", "d", "y"},
			wantAbsent: []string{deletePrompt},
			wantSent: []request{{
				procedure: schedulev1connect.ScheduleServiceDeleteScheduleProcedure,
				msg:       &schedulev1.DeleteScheduleRequest{Id: "s2"},
			}},
		},
		{
			name:       "tab on the list leaves the kind alone",
			press:      []string{"tab", "shift+tab", "esc"},
			want:       []string{"api-refactor"},
			wantAbsent: []string{"weekly-gone"},
		},
		{
			name:       "shift-tab on the row of kinds wraps to the schedules",
			press:      []string{"F", "shift+tab", "esc"},
			want:       []string{"weekly-gone"},
			wantAbsent: []string{"api-refactor"},
		},
		{
			name:       "the row of kinds waits while agents are selected",
			press:      []string{"j", "v", "F", "tab", "shift+tab", "esc"},
			want:       []string{"api-refactor"},
			wantAbsent: []string{"weekly-gone"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openGroupedArchive(t, rows...)

			harness.press(append(tt.press, widenListPresses...)...)

			frame := ansi.Strip(harness.readFrame())
			assert.Equal(t, archiveScreen, harness.readScreen())
			for _, want := range tt.want {
				assert.Contains(t, frame, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, frame, absent)
			}
			harness.assertSent(tt.wantSent...)
		})
	}

	t.Run("the row of kinds refuses the keys while agents are selected", func(t *testing.T) {
		harness := openGroupedArchive(t, rows...)

		harness.press(append(widenListPresses, "j", "v", "F", "?")...)
		frame := harness.readFrame()
		assert.NotEmpty(t, readHoveredRow(frame), "the list keeps the keys")
		assert.NotContains(t, readLegend(frame), "F kind")
	})

	t.Run("the row of kinds holds the keys until escape", func(t *testing.T) {
		harness := openGroupedArchive(t, rows...)

		harness.press(append(widenListPresses, "F")...)
		frame := harness.readFrame()
		legend := readLegend(frame)
		assert.Contains(t, legend, "tab/S-tab switch")
		assert.Contains(t, legend, "esc back to the list")
		assert.Empty(t, readHoveredRow(frame), "the row's arrow stands down")

		harness.press("esc")
		assert.NotEmpty(t, readHoveredRow(harness.readFrame()), "and the list has the keys again")
	})
}

func openGroupedArchive(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{})
	harness.press("shift+tab", "shift+tab", "esc", "f", "shift+tab", "j", "esc")
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

func newArchivedAgentAt(name, at string) *agentv1.Agent {
	agent := newArchivedAgent(name, name, "w1")
	agent.ArchivedAt = xptr.Ptr(at)

	return agent
}

func newArchivedSchedule(id, name string) *schedulev1.Schedule {
	schedule := newSchedule(id, name)
	schedule.ArchivedAt = timestamppb.New(time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC))

	return schedule
}

// TestArchiveLastScreen covers the saved screen drawn under an archived agent.
func TestArchiveLastScreen(t *testing.T) {
	t.Run("the saved screen is asked for once the cursor rests and drawn under the document", func(t *testing.T) {
		harness := openGroupedArchive(t, buildOneArchivedAgent()...)
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
		harness := openGroupedArchive(t, buildOneArchivedAgent()...)
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
		harness := openGroupedArchive(t, buildOneArchivedAgent()...)
		answerLastScreens(harness, nil)

		harness.press("j")
		harness.fireTimers()

		assert.Contains(t, harness.readFrame(), "no saved screen")
	})

	t.Run("a refused read says why", func(t *testing.T) {
		harness := openGroupedArchive(t, buildOneArchivedAgent()...)
		harness.answer(agentv1connect.AgentServiceGetLastScreenProcedure, refuseWith(connect.CodeInternal, "screens exploded"))

		harness.press("j")
		harness.fireTimers()

		frame := harness.readFrame()
		assert.Contains(t, frame, "could not read the saved screen: screens exploded")
		assert.Contains(t, frame, "Restore brings it back")
	})

	t.Run("a folder has no saved screen", func(t *testing.T) {
		harness := openGroupedArchive(t, buildOneArchivedAgent()...)
		harness.fireTimers()

		assert.NotContains(t, harness.readFrame(), "saved screen")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceGetLastScreenProcedure))
	})

	t.Run("an answer for an agent the cursor has left is not drawn", func(t *testing.T) {
		harness := openGroupedArchive(t,
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
		harness := openGroupedArchive(t,
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
		harness := openGroupedArchive(t,
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

// TestArchiveReading covers the keys handed to an archived agent's record.
func TestArchiveReading(t *testing.T) {
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
			harness := openGroupedArchive(t,
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
}
