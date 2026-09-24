package ui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	archivedAt    = "2026-07-30T09:00:00Z"
	sidebarWidth  = 30
	archiveTitle  = "Archive"
	agentsTitle   = "Agents & Workflows"
	deletePrompt  = "Permanently delete"
	emptyingQueue = "Emptying the archive"
)

var widenList = slices.Repeat([]string{"}"}, 10)

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

			harness.press(append(tt.press, widenList...)...)

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

// TestArchiveLegend covers the legend naming what the hovered row takes.
func TestArchiveLegend(t *testing.T) {
	neverStarted := newArchivedAgent("a1", "api-refactor", "w1")
	neverStarted.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE

	tests := []struct {
		name        string
		rows        []proto.Message
		press       []string
		wants       []string
		wantsAbsent []string
	}{
		{
			name:        "on a live folder",
			rows:        buildOneArchivedAgent(),
			wantsAbsent: []string{"restore", "delete for good"},
		},
		{
			name:        "on an archived folder",
			rows:        []proto.Message{newArchivedWorkflow("w1", "backend")},
			wants:       []string{"restore workflow"},
			wantsAbsent: []string{"delete for good"},
		},
		{
			name:        "on an agent that never started",
			rows:        []proto.Message{newLiveWorkflow("w1", "backend"), neverStarted},
			press:       []string{"j"},
			wants:       []string{"restore", "delete for good"},
			wantsAbsent: []string{"restore and open"},
		},
		{
			name:  "on an agent",
			rows:  buildOneArchivedAgent(),
			press: []string{"j"},
			wants: []string{"restore", "restore and open", "delete for good"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openArchive(t, tt.rows...)

			harness.press(append(tt.press, "?")...)

			got := readLegend(harness.readFrame())
			for _, want := range tt.wants {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantsAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// TestArchiveDocument covers what is drawn beside the list for the hovered row.
func TestArchiveDocument(t *testing.T) {
	t.Run("an archived agent is not one merely waiting to be opened", func(t *testing.T) {
		harness := openArchive(t, buildOneArchivedAgent()...)

		harness.press("j")

		frame := harness.readFrame()
		assert.Contains(t, frame, "archived — kept until deleted")
		assert.Contains(t, frame, "Restore brings it back")
		assert.NotContains(t, frame, "dormant")
	})

	t.Run("an agent a delete could not destroy says why", func(t *testing.T) {
		stuck := newArchivedAgent("a1", "api-refactor", "w1")
		stuck.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
		stuck.Metadata = []*agentv1.AgentMetadata{{
			Key:   "error",
			Value: "the worktree is dirty",
		}}
		harness := openArchive(t, newLiveWorkflow("w1", "backend"), stuck)

		harness.press("j")

		frame := harness.readFrame()
		assert.Contains(t, frame, "teardown failed")
		assert.Contains(t, frame, "the worktree is dirty")
		assert.NotContains(t, frame, "Restore brings it back")
	})

	t.Run("the record says what the agents list would say of it", func(t *testing.T) {
		agent := newArchivedAgent("a1", "api-refactor", "w1")
		agent.CreatedAt = "2026-07-29T09:00:00Z"
		agent.Branch = proto.String("agent/a1")
		agent.RepositoryUrl = proto.String("https://github.com/harrisoncramer/weave")
		harness := openArchive(t,
			newLiveWorkflow("w1", "backend"),
			agent,
			&pullrequestv1.PullRequest{
				Id:                "pr-42",
				RepositoryUrl:     "https://github.com/harrisoncramer/weave",
				HeadRepositoryUrl: "https://github.com/harrisoncramer/weave",
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

		harness.press(append([]string{"j", "ctrl+t"}, widenList...)...)

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
			assert.LessOrEqual(t, ansi.StringWidth(row), 140, "neither column runs past the window")
		}
	})

	t.Run("an empty archive says what the archive is for", func(t *testing.T) {
		harness := openArchive(t, newLiveWorkflow("w1", "backend"))

		frame := harness.readFrame()
		assert.Contains(t, frame, "nothing archived")
		assert.Contains(t, frame, "until you delete them")
	})
}

// TestArchiveSort covers ordering the archive by its column tabs.
func TestArchiveSort(t *testing.T) {
	sorted := []proto.Message{
		newLiveWorkflow("w1", "backend"),
		newArchivedAgentAt("zeta", "w1", "2026-07-30T12:00:00Z"),
		newArchivedAgentAt("mid", "w1", "2026-07-30T13:00:00Z"),
		newArchivedAgentAt("alpha", "w1", "2026-07-30T09:00:00Z"),
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
			harness := openArchive(t, sorted...)

			harness.press(tt.press...)

			assert.Equal(t, tt.want, readListRows(harness.readFrame()))
		})
	}

	t.Run("the cursor keeps its row as the archive is reordered", func(t *testing.T) {
		harness := openArchive(t, sorted...)
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

	t.Run("a short window keeps the list beside the document", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{
			width:  120,
			height: 10,
		})
		harness.watch(buildSnapshot(buildOneArchivedAgent()...))

		harness.press("Z", "?")

		frame := harness.readFrame()
		assert.Len(t, splitLines(frame), 10, "the view is exactly the pane")
		assert.Contains(t, frame, "show help", "the legend gave up its expansion for the rows")
		assert.NotContains(t, frame, "hide help")
	})

	t.Run("the document column stands as tall as the window", func(t *testing.T) {
		workflow := newLiveWorkflow("w1", "backend")
		workflow.Context = strings.Repeat("a line of the document. ", 60)

		for _, height := range []int{12, 16, 24} {
			harness := newPanelHarness(t, panelHarnessParams{
				width:  140,
				height: height,
			})
			harness.watch(buildSnapshot(workflow, newArchivedAgent("a1", "api-refactor", "w1")))

			harness.press("Z")

			assert.Len(t, splitLines(harness.readFrame()), height, "height=%d", height)
		}
	})

	t.Run("the list holds the keyboard as the archive opens", func(t *testing.T) {
		harness := openArchive(t, buildOneArchivedAgent()...)

		harness.press("j", "r")

		harness.assertSent(request{
			procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
			msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
		})
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

func newArchivedAgentAt(name, workflowID, at string) *agentv1.Agent {
	agent := newArchivedAgent(name, name, workflowID)
	agent.ArchivedAt = proto.String(at)

	return agent
}

func splitLines(frame string) []string {
	return strings.Split(frame, "\n")
}

func readListRows(frame string) []string {
	var rows []string
	for _, line := range splitLines(frame) {
		_, row, isRow := strings.Cut(ansi.Truncate(line, sidebarWidth, ""), "│ ")
		if isRow {
			rows = append(rows, strings.TrimRight(row, " "))
		}
	}

	return rows
}

func readHoveredRow(frame string) string {
	for _, line := range splitLines(frame) {
		if !strings.HasPrefix(line, "›") {
			continue
		}

		_, row, _ := strings.Cut(ansi.Truncate(line, sidebarWidth, ""), "│ ")

		return strings.TrimRight(row, " ")
	}

	return ""
}

func findRecordLine(frame, label string) int {
	for i, line := range splitLines(frame) {
		if strings.HasPrefix(strings.TrimLeft(line, "› "), label+" ") {
			return i
		}
	}

	return -1
}

func readLegend(frame string) string {
	rows := splitLines(frame)
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.HasPrefix(rows[i], "─") {
			return strings.Join(rows[i+1:], "\n")
		}
	}

	return ""
}
