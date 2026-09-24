package ui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"komrade/internal/ui/paint"
	agentv1 "komrade/proto/gen/agent/v1"
	"komrade/proto/gen/agent/v1/agentv1connect"
	pullrequestv1 "komrade/proto/gen/pullrequest/v1"
	workflowv1 "komrade/proto/gen/workflow/v1"
	"komrade/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	archivedAt    = "2026-07-30T09:00:00Z"
	archiveTitle  = "Archive"
	agentsTitle   = "Agents & Workflows"
	deletePrompt  = "Permanently delete"
	emptyingQueue = "Emptying the archive"
)

var widenListPresses = slices.Repeat([]string{"}"}, 10)

// TestArchiveKeys covers what each key does on the row under the cursor.
func TestArchiveKeys(t *testing.T) {
	neverStarted := newArchivedAgent("a1", "api-refactor", "w1")
	neverStarted.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE

	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		wantTitle  string
		wantPrompt string
		wantSent   []request
	}{
		{
			name:      "restore on an empty archive asks for no write",
			rows:      []proto.Message{newLiveWorkflow("w1", "backend")},
			press:     []string{"r"},
			wantTitle: archiveTitle,
		},
		{
			name:      "delete on an empty archive opens no prompt",
			rows:      []proto.Message{newLiveWorkflow("w1", "backend")},
			press:     []string{"d"},
			wantTitle: archiveTitle,
		},
		{
			name:      "empty on an empty archive opens no prompt",
			rows:      []proto.Message{newLiveWorkflow("w1", "backend")},
			press:     []string{"D"},
			wantTitle: archiveTitle,
		},
		{
			name:      "restore on a live folder's header asks for no write",
			rows:      buildOneArchivedAgent(),
			press:     []string{"r"},
			wantTitle: archiveTitle,
		},
		{
			name:      "delete on a live folder's header opens no prompt",
			rows:      buildOneArchivedAgent(),
			press:     []string{"d"},
			wantTitle: archiveTitle,
		},
		{
			name:      "restore and open passes over a folder",
			rows:      buildOneArchivedAgent(),
			press:     []string{"R"},
			wantTitle: archiveTitle,
		},
		{
			name:      "restore and open passes over an agent that never started",
			rows:      []proto.Message{newLiveWorkflow("w1", "backend"), neverStarted},
			press:     []string{"j", "R"},
			wantTitle: archiveTitle,
		},
		{
			name:      "back returns to the agents tree",
			rows:      buildOneArchivedAgent(),
			press:     []string{"j", "esc"},
			wantTitle: agentsTitle,
		},
		{
			name:      "restore puts the hovered agent back and stays on the archive",
			rows:      buildOneArchivedAgent(),
			press:     []string{"j", "r"},
			wantTitle: archiveTitle,
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:      "restore and open puts the agent back and opens it",
			rows:      buildOneArchivedAgent(),
			press:     []string{"j", "R"},
			wantTitle: agentsTitle,
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
			name:      "restore on an archived folder restores the workflow",
			rows:      []proto.Message{newArchivedWorkflow("w1", "backend")},
			press:     []string{"r"},
			wantTitle: archiveTitle,
			wantSent: []request{{
				procedure: workflowv1connect.WorkflowServiceRestoreWorkflowProcedure,
				msg:       &workflowv1.RestoreWorkflowRequest{Id: "w1"},
			}},
		},
		{
			name:       "delete on an agent opens the confirm prompt rather than destroying",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "d"},
			wantTitle:  archiveTitle,
			wantPrompt: `Permanently delete agent "api-refactor"?`,
		},
		{
			name:       "delete on an archived folder opens the confirm prompt",
			rows:       []proto.Message{newArchivedWorkflow("w1", "backend")},
			press:      []string{"d"},
			wantTitle:  archiveTitle,
			wantPrompt: `Permanently delete workflow "backend"?`,
		},
		{
			name:       "delete on an archived folder says the agent inside it goes too",
			rows:       []proto.Message{newArchivedWorkflow("w1", "backend"), newArchivedAgent("a1", "api-refactor", "w1")},
			press:      []string{"d"},
			wantTitle:  archiveTitle,
			wantPrompt: "The one agent still archived inside it goes with it",
		},
		{
			name:       "empty on an agent opens the confirm prompt rather than sweeping",
			rows:       buildOneArchivedAgent(),
			press:      []string{"j", "D"},
			wantTitle:  archiveTitle,
			wantPrompt: "Permanently delete the one archived agent?",
		},
		{
			name:       "empty on a header opens it too",
			rows:       buildOneArchivedAgent(),
			press:      []string{"D"},
			wantTitle:  archiveTitle,
			wantPrompt: "Permanently delete the one archived agent?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchive(t, tt.rows...)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Contains(t, splitLines(frame)[0], tt.wantTitle)
			if tt.wantPrompt == "" {
				assert.NotContains(t, frame, deletePrompt)
			} else {
				assert.Contains(t, frame, tt.wantPrompt)
			}
			harness.assertSent(tt.wantSent...)
		})
	}
}

// TestArchivePrompts covers answering the delete and empty prompts.
func TestArchivePrompts(t *testing.T) {
	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		wantSent   []request
		wantBanner string
	}{
		{
			name:  "declining the delete destroys nothing",
			rows:  buildOneArchivedAgent(),
			press: []string{"j", "d", "n"},
		},
		{
			name:  "declining the workflow delete destroys nothing",
			rows:  []proto.Message{newArchivedWorkflow("w1", "backend")},
			press: []string{"d", "n"},
		},
		{
			name:  "declining the empty queues nothing",
			rows:  buildOneArchivedAgent(),
			press: []string{"D", "n"},
		},
		{
			name:  "the empty prompt starts on cancel",
			rows:  buildOneArchivedAgent(),
			press: []string{"D", "ctrl+s"},
		},
		{
			name:  "confirming the delete destroys the agent it was opened on",
			rows:  buildOneArchivedAgent(),
			press: []string{"j", "d", "y"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
				msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:  "confirming the workflow delete destroys the workflow it was opened on",
			rows:  []proto.Message{newArchivedWorkflow("w1", "backend")},
			press: []string{"d", "y"},
			wantSent: []request{{
				procedure: workflowv1connect.WorkflowServiceDeleteWorkflowProcedure,
				msg:       &workflowv1.DeleteWorkflowRequest{Id: "w1"},
			}},
		},
		{
			name:  "confirming the empty queues the sweep and says so",
			rows:  buildOneArchivedAgent(),
			press: []string{"D", "y"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceEmptyArchiveProcedure,
				msg:       &emptypb.Empty{},
			}},
			wantBanner: emptyingQueue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchive(t, tt.rows...)

			harness.press(tt.press...)

			frame := harness.readFrame()
			assert.Contains(t, splitLines(frame)[0], archiveTitle, "the prompt returns to the archive it was opened from")
			assert.NotContains(t, frame, deletePrompt, "the prompt is down")
			if tt.wantBanner == "" {
				assert.NotContains(t, frame, emptyingQueue)
			} else {
				assert.Contains(t, frame, tt.wantBanner)
			}
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
			press:       []string{"j", "r"},
			wantSummary: `Could not restore agent "api-refactor"`,
		},
		{
			name:        "a refused workflow restore",
			rows:        []proto.Message{newArchivedWorkflow("w1", "backend")},
			refused:     workflowv1connect.WorkflowServiceRestoreWorkflowProcedure,
			press:       []string{"r"},
			wantSummary: `Could not restore workflow "backend"`,
		},
		{
			name:        "a refused restore and open",
			rows:        buildOneArchivedAgent(),
			refused:     agentv1connect.AgentServiceRestoreAgentsProcedure,
			press:       []string{"j", "R"},
			wantSummary: `Could not restore and open agent "api-refactor"`,
		},
		{
			name:        "a refused delete",
			rows:        buildOneArchivedAgent(),
			refused:     agentv1connect.AgentServiceDeleteAgentsProcedure,
			press:       []string{"j", "d", "y"},
			wantSummary: `Could not delete agent "api-refactor"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchive(t, tt.rows...)
			harness.refuse(tt.refused, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

			harness.press(append(tt.press, widenListPresses...)...)

			frame := harness.readFrame()
			assert.Contains(t, frame, tt.wantSummary)
			assert.Contains(t, frame, "no such agent")
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
					ArchivedAt: proto.String(archivedAt),
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
			harness := openArchive(t, tt.rows...)

			assert.Equal(t, tt.wantRows, readListRows(harness.readFrame()))
		})
	}
}

// TestArchiveRowEmphasis covers what the archive's rows say in colour and in their bar.
func TestArchiveRowEmphasis(t *testing.T) {
	t.Run("an archived folder is greyed and a live one is not", func(t *testing.T) {
		harness := openArchive(t,
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
		harness := openArchive(t, newLiveWorkflow("w1", "backend"), newArchivedAgent("a1", "auth", "w1"), stuck)

		frame := harness.readFrame()

		assert.Contains(t, findLine(splitLines(frame), "auth"), "│   auth")
		assert.Contains(t, findLine(splitLines(frame), "stuck"), "┃   stuck")
	})
}

// TestArchiveLegend covers the legend naming what the hovered row takes.
func TestArchiveLegend(t *testing.T) {
	neverStarted := newArchivedAgent("a1", "api-refactor", "w1")
	neverStarted.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE

	tests := []struct {
		name       string
		rows       []proto.Message
		press      []string
		want       []string
		wantAbsent []string
	}{
		{
			name:       "on a live folder",
			rows:       buildOneArchivedAgent(),
			wantAbsent: []string{"restore", "delete for good"},
		},
		{
			name:       "on an archived folder",
			rows:       []proto.Message{newArchivedWorkflow("w1", "backend")},
			want:       []string{"restore workflow"},
			wantAbsent: []string{"delete for good"},
		},
		{
			name:       "on an agent that never started",
			rows:       []proto.Message{newLiveWorkflow("w1", "backend"), neverStarted},
			press:      []string{"j"},
			want:       []string{"restore", "delete for good"},
			wantAbsent: []string{"restore and open"},
		},
		{
			name:  "on an agent",
			rows:  buildOneArchivedAgent(),
			press: []string{"j"},
			want:  []string{"restore", "restore and open", "delete for good"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchive(t, tt.rows...)

			harness.press(append(tt.press, "?")...)

			got := readLegend(harness.readFrame())
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// TestArchiveDocument covers what is drawn beside the list for the hovered row.
func TestArchiveDocument(t *testing.T) {
	stuck := newArchivedAgent("a1", "api-refactor", "w1")
	stuck.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
	stuck.Metadata = []*agentv1.AgentMetadata{{
		Key:   "error",
		Value: "the worktree is dirty",
	}}

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchive(t, tt.rows...)

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
		agent.Branch = proto.String("agent/a1")
		agent.RepositoryUrl = proto.String("https://github.com/harrisoncramer/komrade")
		harness := openArchive(t,
			newLiveWorkflow("w1", "backend"),
			agent,
			&pullrequestv1.PullRequest{
				Id:                "pr-42",
				RepositoryUrl:     "https://github.com/harrisoncramer/komrade",
				HeadRepositoryUrl: "https://github.com/harrisoncramer/komrade",
				HeadBranch:        "agent/a1",
				Number:            42,
				State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
				OpenedAt:          "2026-07-30T08:00:00Z",
				MergedAt:          proto.String(archivedAt),
				ClosedAt:          proto.String(archivedAt),
			},
			&agentv1.WatchedReview{
				Id:           "v1",
				AgentId:      "a1",
				RoundId:      "a1-round-1",
				RoundOrdinal: 1,
				ReleasedAt:   archivedAt,
				Verdict:      agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
			},
		)

		harness.press(append([]string{"j", "ctrl+t"}, widenListPresses...)...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "agent/a1")
		assert.Contains(t, frame, "#42 merged")
		assert.Contains(t, frame, "approved while archived")
		created, archived := findRecordLine(frame, "created"), findRecordLine(frame, "archived")
		require.Positive(t, created)
		assert.Less(t, created, archived, "when it was put away comes after when it was made")
	})

	t.Run("the document follows the cursor onto the folder", func(t *testing.T) {
		workflow := newLiveWorkflow("w1", "backend")
		workflow.Context = "the shared notes"
		harness := openArchive(t, workflow, newArchivedAgent("a1", "api-refactor", "w1"))

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
			harness := openArchive(t, unsorted...)

			harness.press(tt.press...)

			assert.Equal(t, tt.want, readListRows(harness.readFrame()))
		})
	}

	t.Run("the top and bottom keys walk the archive's own rows", func(t *testing.T) {
		harness := openArchive(t, unsorted...)

		harness.press("G")
		assert.Equal(t, "  alpha", readHoveredRow(harness.readFrame()))

		harness.press("g")
		assert.Equal(t, "  alpha", readHoveredRow(harness.readFrame()), "the first press waits for the second")

		harness.press("g")
		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))
	})

	t.Run("the cursor keeps its row as the archive is reordered", func(t *testing.T) {
		harness := openArchive(t, unsorted...)
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
		harness := openArchive(t, buildOneArchivedAgent()...)

		assert.Contains(t, harness.readFrame(), "api-refactor")
		harness.assertSent()
	})

	t.Run("the archive takes the window and gives it back for a running agent", func(t *testing.T) {
		harness := openArchive(t,
			newLiveWorkflow("w1", "backend"),
			&agentv1.Agent{
				Id:         "a2",
				Name:       "auth",
				WorkflowId: "w1",
				PaneId:     "%9",
				Alive:      proto.Bool(true),
				State:      agentv1.AgentState_AGENT_STATE_IDLE,
			},
			newArchivedAgent("a1", "api-refactor", "w1"),
		)
		require.True(t, harness.panel.IsZoomed(), "the archive draws both columns itself")

		harness.press("esc")

		assert.False(t, harness.panel.IsZoomed(), "leaving the archive for a running agent gives the other panes their share back")
	})

	t.Run("restoring and opening an agent hands the window to the pane it opens", func(t *testing.T) {
		harness := openArchive(t, buildOneArchivedAgent()...)
		require.True(t, harness.panel.IsZoomed())
		answerResume(harness)

		harness.press("j", "R")

		assert.False(t, harness.panel.IsZoomed(), "the pane it made is not left under the archive's own zoom")
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
	})

	t.Run("a short window keeps the list beside the document", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{
			width:  120,
			height: 10,
		})
		harness.watch(buildSnapshot(buildOneArchivedAgent()...))

		harness.press("Z", "?")

		frame := harness.readFrame()
		assert.Equal(t, []string{"backend", "  api-refactor"}, readListRows(frame), "the list keeps its column")
		assert.Contains(t, frame, "no shared context set", "beside the hovered row's document")
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

				harness.press("Z")

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
			press: []string{"v", "r"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"auth", "api", "db"}},
			}},
		},
		{
			name:  "the range key reaches back to the nearest selection",
			press: []string{"j", "v", "j", "j", "V", "r"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"auth", "api", "db"}},
			}},
		},
		{
			name:  "restore takes the whole selection rather than the hovered row",
			press: []string{"j", "v", "j", "v", "j", "r"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"auth", "api"}},
			}},
		},
		{
			name:  "delete opens its prompt over the archive on the whole selection",
			press: []string{"j", "v", "j", "v", "j", "d", "y"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
				msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"auth", "api"}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchive(t, rows...)

			harness.press(tt.press...)

			assert.Contains(t, splitLines(harness.readFrame())[0], archiveTitle)
			harness.assertSent(tt.wantSent...)
		})
	}

	t.Run("a folder restore stands down while a selection is up", func(t *testing.T) {
		harness := openArchive(t, newArchivedWorkflow("w1", "backend"), newArchivedAgent("a1", "auth", "w1"))

		harness.press("j", "v", "g", "g", "r")

		harness.assertSent(request{
			procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
			msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
		})
	})

	t.Run("the selection outlives a refused restore", func(t *testing.T) {
		harness := openArchive(t, rows...)
		harness.refuse(agentv1connect.AgentServiceRestoreAgentsProcedure, connect.NewError(connect.CodeUnavailable, errors.New("down")))

		harness.press("j", "v", "r")

		assert.Contains(t, readLegend(harness.readFrame()), "1 selected")
	})

	t.Run("escape clears the selection before it leaves the archive", func(t *testing.T) {
		harness := openArchive(t, rows...)
		harness.press("j", "v")

		harness.press("esc")

		frame := harness.readFrame()
		assert.Contains(t, splitLines(frame)[0], archiveTitle)
		assert.NotContains(t, readLegend(frame), "selected")

		harness.press("esc")
		assert.Contains(t, splitLines(harness.readFrame())[0], agentsTitle)
	})

	t.Run("an agent gone from the archive drops out of the selection", func(t *testing.T) {
		harness := openArchive(t, rows...)
		harness.press("j", "v", "G", "v")

		harness.watch(buildSnapshot(rows[0], rows[3]))
		harness.press("r")

		harness.assertSent(request{
			procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
			msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"db"}},
		})
	})

	t.Run("selecting in the archive leaves the agents tree alone", func(t *testing.T) {
		harness := openArchive(t, append(slices.Clone(rows), newAgent("a9", "live", "w1"))...)
		harness.press("j", "v", "esc", "esc")

		assert.NotContains(t, readLegend(harness.readFrame()), "selected")
	})
}

func openArchive(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{})
	harness.watch(buildSnapshot(rows...))
	harness.press("Z")

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
		ArchivedAt: proto.String(archivedAt),
	}
}

func newArchivedAgent(id, name, workflowID string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         id,
		Name:       name,
		WorkflowId: workflowID,
		State:      agentv1.AgentState_AGENT_STATE_IDLE,
		ArchivedAt: proto.String(archivedAt),
	}
}

func newArchivedAgentAt(name, at string) *agentv1.Agent {
	agent := newArchivedAgent(name, name, "w1")
	agent.ArchivedAt = proto.String(at)

	return agent
}
