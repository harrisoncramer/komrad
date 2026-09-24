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

func TestArchiveKeys(t *testing.T) {
	neverStarted := archivedAgent("a1", "api-refactor", "w1")
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
			rows:      []proto.Message{liveWorkflow("w1", "backend")},
			press:     []string{"r"},
			wantTitle: archiveTitle,
		},
		{
			name:      "delete on an empty archive opens no prompt",
			rows:      []proto.Message{liveWorkflow("w1", "backend")},
			press:     []string{"d"},
			wantTitle: archiveTitle,
		},
		{
			name:      "empty on an empty archive opens no prompt",
			rows:      []proto.Message{liveWorkflow("w1", "backend")},
			press:     []string{"D"},
			wantTitle: archiveTitle,
		},
		{
			name:      "restore on a live folder's header asks for no write",
			rows:      oneArchivedAgent(),
			press:     []string{"r"},
			wantTitle: archiveTitle,
		},
		{
			name:      "delete on a live folder's header opens no prompt",
			rows:      oneArchivedAgent(),
			press:     []string{"d"},
			wantTitle: archiveTitle,
		},
		{
			name:      "restore and open passes over a folder",
			rows:      oneArchivedAgent(),
			press:     []string{"R"},
			wantTitle: archiveTitle,
		},
		{
			name:      "restore and open passes over an agent that never started",
			rows:      []proto.Message{liveWorkflow("w1", "backend"), neverStarted},
			press:     []string{"j", "R"},
			wantTitle: archiveTitle,
		},
		{
			name:      "back returns to the agents tree",
			rows:      oneArchivedAgent(),
			press:     []string{"j", "esc"},
			wantTitle: agentsTitle,
		},
		{
			name:      "restore puts the hovered agent back and stays on the archive",
			rows:      oneArchivedAgent(),
			press:     []string{"j", "r"},
			wantTitle: archiveTitle,
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
				msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:      "restore and open puts the agent back and opens it",
			rows:      oneArchivedAgent(),
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
			rows:      []proto.Message{archivedWorkflow("w1", "backend")},
			press:     []string{"r"},
			wantTitle: archiveTitle,
			wantSent: []request{{
				procedure: workflowv1connect.WorkflowServiceRestoreWorkflowProcedure,
				msg:       &workflowv1.RestoreWorkflowRequest{Id: "w1"},
			}},
		},
		{
			name:       "delete on an agent opens the confirm prompt rather than destroying",
			rows:       oneArchivedAgent(),
			press:      []string{"j", "d"},
			wantTitle:  archiveTitle,
			wantPrompt: `Permanently delete agent "api-refactor"?`,
		},
		{
			name:       "delete on an archived folder opens the confirm prompt",
			rows:       []proto.Message{archivedWorkflow("w1", "backend")},
			press:      []string{"d"},
			wantTitle:  archiveTitle,
			wantPrompt: `Permanently delete workflow "backend"?`,
		},
		{
			name:       "empty on an agent opens the confirm prompt rather than sweeping",
			rows:       oneArchivedAgent(),
			press:      []string{"j", "D"},
			wantTitle:  archiveTitle,
			wantPrompt: "Permanently delete the one archived agent?",
		},
		{
			name:       "empty on a header opens it too",
			rows:       oneArchivedAgent(),
			press:      []string{"D"},
			wantTitle:  archiveTitle,
			wantPrompt: "Permanently delete the one archived agent?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := openArchive(t, tt.rows...)

			h.press(tt.press...)

			frame := h.frame()
			assert.Contains(t, lines(frame)[0], tt.wantTitle)
			if tt.wantPrompt == "" {
				assert.NotContains(t, frame, deletePrompt)
			} else {
				assert.Contains(t, frame, tt.wantPrompt)
			}
			h.assertSent(tt.wantSent...)
		})
	}
}

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
			rows:  oneArchivedAgent(),
			press: []string{"j", "d", "n"},
		},
		{
			name:  "declining the workflow delete destroys nothing",
			rows:  []proto.Message{archivedWorkflow("w1", "backend")},
			press: []string{"d", "n"},
		},
		{
			name:  "declining the empty queues nothing",
			rows:  oneArchivedAgent(),
			press: []string{"D", "n"},
		},
		{
			name:  "the empty prompt starts on cancel",
			rows:  oneArchivedAgent(),
			press: []string{"D", "ctrl+s"},
		},
		{
			name:  "confirming the delete destroys the agent it was opened on",
			rows:  oneArchivedAgent(),
			press: []string{"j", "d", "y"},
			wantSent: []request{{
				procedure: agentv1connect.AgentServiceDeleteAgentsProcedure,
				msg:       &agentv1.DeleteAgentsRequest{Ids: []string{"a1"}},
			}},
		},
		{
			name:  "confirming the workflow delete destroys the workflow it was opened on",
			rows:  []proto.Message{archivedWorkflow("w1", "backend")},
			press: []string{"d", "y"},
			wantSent: []request{{
				procedure: workflowv1connect.WorkflowServiceDeleteWorkflowProcedure,
				msg:       &workflowv1.DeleteWorkflowRequest{Id: "w1"},
			}},
		},
		{
			name:  "confirming the empty queues the sweep and says so",
			rows:  oneArchivedAgent(),
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
			h := openArchive(t, tt.rows...)

			h.press(tt.press...)

			frame := h.frame()
			assert.Contains(t, lines(frame)[0], archiveTitle, "the prompt returns to the archive it was opened from")
			assert.NotContains(t, frame, deletePrompt, "the prompt is down")
			if tt.wantBanner == "" {
				assert.NotContains(t, frame, emptyingQueue)
			} else {
				assert.Contains(t, frame, tt.wantBanner)
			}
			h.assertSent(tt.wantSent...)
		})
	}
}

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
			rows:        oneArchivedAgent(),
			refused:     agentv1connect.AgentServiceRestoreAgentsProcedure,
			press:       []string{"j", "r"},
			wantSummary: `Could not restore agent "api-refactor"`,
		},
		{
			name:        "a refused workflow restore",
			rows:        []proto.Message{archivedWorkflow("w1", "backend")},
			refused:     workflowv1connect.WorkflowServiceRestoreWorkflowProcedure,
			press:       []string{"r"},
			wantSummary: `Could not restore workflow "backend"`,
		},
		{
			name:        "a refused restore and open",
			rows:        oneArchivedAgent(),
			refused:     agentv1connect.AgentServiceRestoreAgentsProcedure,
			press:       []string{"j", "R"},
			wantSummary: `Could not restore and open agent "api-refactor"`,
		},
		{
			name:        "a refused delete",
			rows:        oneArchivedAgent(),
			refused:     agentv1connect.AgentServiceDeleteAgentsProcedure,
			press:       []string{"j", "d", "y"},
			wantSummary: `Could not delete agent "api-refactor"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := openArchive(t, tt.rows...)
			h.refuse(tt.refused, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

			h.press(append(tt.press, widenList...)...)

			frame := h.frame()
			assert.Contains(t, frame, tt.wantSummary)
			assert.Contains(t, frame, "no such agent")
		})
	}
}

func TestArchiveRows(t *testing.T) {
	tests := []struct {
		name     string
		rows     []proto.Message
		wantRows []string
	}{
		{
			name: "nothing archived draws nothing",
			rows: []proto.Message{liveWorkflow("w1", "backend")},
		},
		{
			name:     "an archived folder emptied of its agents keeps its header",
			rows:     []proto.Message{archivedWorkflow("w1", "backend")},
			wantRows: []string{"backend"},
		},
		{
			name: "a live folder with nothing put away out of it gets no header",
			rows: []proto.Message{
				liveWorkflow("w1", "backend"),
				liveWorkflow("w2", "frontend"),
				archivedAgent("a1", "auth", "w1"),
			},
			wantRows: []string{"backend", "  auth"},
		},
		{
			name: "live and archived folders are drawn together by name",
			rows: []proto.Message{
				liveWorkflow("w2", "frontend"),
				archivedWorkflow("w1", "backend"),
				archivedWorkflow("w3", "infra"),
				archivedAgent("a1", "auth", "w1"),
				archivedAgent("a2", "ui", "w2"),
			},
			wantRows: []string{"backend", "  auth", "frontend", "  ui", "infra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := openArchive(t, tt.rows...)

			assert.Equal(t, tt.wantRows, listRows(h.frame()))
		})
	}
}

func TestArchiveLegend(t *testing.T) {
	neverStarted := archivedAgent("a1", "api-refactor", "w1")
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
			rows:        oneArchivedAgent(),
			wantsAbsent: []string{"restore", "delete for good"},
		},
		{
			name:        "on an archived folder",
			rows:        []proto.Message{archivedWorkflow("w1", "backend")},
			wants:       []string{"restore workflow"},
			wantsAbsent: []string{"delete for good"},
		},
		{
			name:        "on an agent that never started",
			rows:        []proto.Message{liveWorkflow("w1", "backend"), neverStarted},
			press:       []string{"j"},
			wants:       []string{"restore", "delete for good"},
			wantsAbsent: []string{"restore and open"},
		},
		{
			name:  "on an agent",
			rows:  oneArchivedAgent(),
			press: []string{"j"},
			wants: []string{"restore", "restore and open", "delete for good"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := openArchive(t, tt.rows...)

			h.press(append(tt.press, "?")...)

			got := legend(h.frame())
			for _, want := range tt.wants {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantsAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

func TestArchiveDocument(t *testing.T) {
	t.Run("an archived agent is not one merely waiting to be opened", func(t *testing.T) {
		h := openArchive(t, oneArchivedAgent()...)

		h.press("j")

		frame := h.frame()
		assert.Contains(t, frame, "archived — kept until deleted")
		assert.Contains(t, frame, "Restore brings it back")
		assert.NotContains(t, frame, "dormant")
	})

	t.Run("an agent a delete could not destroy says why", func(t *testing.T) {
		stuck := archivedAgent("a1", "api-refactor", "w1")
		stuck.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP
		stuck.Metadata = []*agentv1.AgentMetadata{{
			Key:   "error",
			Value: "the worktree is dirty",
		}}
		h := openArchive(t, liveWorkflow("w1", "backend"), stuck)

		h.press("j")

		frame := h.frame()
		assert.Contains(t, frame, "teardown failed")
		assert.Contains(t, frame, "the worktree is dirty")
		assert.NotContains(t, frame, "Restore brings it back")
	})

	t.Run("the record says what the agents list would say of it", func(t *testing.T) {
		agent := archivedAgent("a1", "api-refactor", "w1")
		agent.CreatedAt = "2026-07-29T09:00:00Z"
		agent.Branch = proto.String("agent/a1")
		agent.RepositoryUrl = proto.String("https://github.com/harrisoncramer/weave")
		h := openArchive(t,
			liveWorkflow("w1", "backend"),
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

		h.press(append([]string{"j", "ctrl+t"}, widenList...)...)

		frame := h.frame()
		assert.Contains(t, frame, "agent/a1")
		assert.Contains(t, frame, "#42 merged")
		assert.Contains(t, frame, "approved while archived")
		created, archived := recordLine(frame, "created"), recordLine(frame, "archived")
		require.Positive(t, created)
		assert.Less(t, created, archived, "when it was put away comes after when it was made")
	})

	t.Run("the document follows the cursor onto the folder", func(t *testing.T) {
		workflow := liveWorkflow("w1", "backend")
		workflow.Context = "the shared notes"
		h := openArchive(t, workflow, archivedAgent("a1", "api-refactor", "w1"))

		frame := h.frame()
		assert.Contains(t, frame, "the shared notes")
		for _, row := range lines(frame) {
			assert.LessOrEqual(t, ansi.StringWidth(row), 140, "neither column runs past the window")
		}
	})

	t.Run("an empty archive says what the archive is for", func(t *testing.T) {
		h := openArchive(t, liveWorkflow("w1", "backend"))

		frame := h.frame()
		assert.Contains(t, frame, "nothing archived")
		assert.Contains(t, frame, "until you delete them")
	})
}

func TestArchiveSort(t *testing.T) {
	sorted := []proto.Message{
		liveWorkflow("w1", "backend"),
		archivedAgentAt("zeta", "w1", "2026-07-30T12:00:00Z"),
		archivedAgentAt("mid", "w1", "2026-07-30T13:00:00Z"),
		archivedAgentAt("alpha", "w1", "2026-07-30T09:00:00Z"),
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
			h := openArchive(t, sorted...)

			h.press(tt.press...)

			assert.Equal(t, tt.want, listRows(h.frame()))
		})
	}

	t.Run("the cursor keeps its row as the archive is reordered", func(t *testing.T) {
		h := openArchive(t, sorted...)
		h.press("j", "j", "j")
		require.Equal(t, "  alpha", hoveredRow(h.frame()))

		h.press("f", "tab", "k", "esc")
		assert.Equal(t, "  alpha", hoveredRow(h.frame()), "name up")

		h.press("f", "j", "esc")
		assert.Equal(t, "  alpha", hoveredRow(h.frame()), "name down")
	})
}

func TestArchiveWindow(t *testing.T) {
	t.Run("entering the archive asks for nothing", func(t *testing.T) {
		h := openArchive(t, oneArchivedAgent()...)

		assert.Contains(t, h.frame(), "api-refactor")
		h.assertSent()
	})

	t.Run("the archive takes the window and gives it back for a running agent", func(t *testing.T) {
		h := openArchive(t,
			liveWorkflow("w1", "backend"),
			&agentv1.Agent{
				Id:         "a2",
				Name:       "auth",
				WorkflowId: "w1",
				PaneId:     "%9",
				Alive:      proto.Bool(true),
				State:      agentv1.AgentState_AGENT_STATE_IDLE,
			},
			archivedAgent("a1", "api-refactor", "w1"),
		)
		require.True(t, h.panel.IsZoomed(), "the archive draws both columns itself")

		h.press("esc")

		assert.False(t, h.panel.IsZoomed(), "leaving the archive for a running agent gives the other panes their share back")
	})

	t.Run("a short window keeps the list beside the document", func(t *testing.T) {
		h := newPanelHarness(t, panelHarnessParams{
			width:  120,
			height: 10,
		})
		h.watch(snapshot(oneArchivedAgent()...))

		h.press("Z", "?")

		frame := h.frame()
		assert.Len(t, lines(frame), 10, "the view is exactly the pane")
		assert.Contains(t, frame, "show help", "the legend gave up its expansion for the rows")
		assert.NotContains(t, frame, "hide help")
	})

	t.Run("the document column stands as tall as the window", func(t *testing.T) {
		workflow := liveWorkflow("w1", "backend")
		workflow.Context = strings.Repeat("a line of the document. ", 60)

		for _, height := range []int{12, 16, 24} {
			h := newPanelHarness(t, panelHarnessParams{
				width:  140,
				height: height,
			})
			h.watch(snapshot(workflow, archivedAgent("a1", "api-refactor", "w1")))

			h.press("Z")

			assert.Len(t, lines(h.frame()), height, "height=%d", height)
		}
	})

	t.Run("the list holds the keyboard as the archive opens", func(t *testing.T) {
		h := openArchive(t, oneArchivedAgent()...)

		h.press("j", "r")

		h.assertSent(request{
			procedure: agentv1connect.AgentServiceRestoreAgentsProcedure,
			msg:       &agentv1.RestoreAgentsRequest{Ids: []string{"a1"}},
		})
	})
}

func openArchive(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	h := newPanelHarness(t, panelHarnessParams{})
	h.watch(snapshot(rows...))
	h.press("Z")

	return h
}

func oneArchivedAgent() []proto.Message {
	return []proto.Message{
		liveWorkflow("w1", "backend"),
		archivedAgent("a1", "api-refactor", "w1"),
	}
}

func liveWorkflow(id, name string) *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:   id,
		Name: name,
	}
}

func archivedWorkflow(id, name string) *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:         id,
		Name:       name,
		ArchivedAt: proto.String(archivedAt),
	}
}

func archivedAgent(id, name, workflowID string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         id,
		Name:       name,
		WorkflowId: workflowID,
		State:      agentv1.AgentState_AGENT_STATE_IDLE,
		ArchivedAt: proto.String(archivedAt),
	}
}

func archivedAgentAt(name, workflowID, at string) *agentv1.Agent {
	agent := archivedAgent(name, name, workflowID)
	agent.ArchivedAt = proto.String(at)

	return agent
}

func lines(frame string) []string {
	return strings.Split(frame, "\n")
}

func listRows(frame string) []string {
	var rows []string
	for _, line := range lines(frame) {
		_, row, isRow := strings.Cut(ansi.Truncate(line, sidebarWidth, ""), "│ ")
		if isRow {
			rows = append(rows, strings.TrimRight(row, " "))
		}
	}

	return rows
}

func hoveredRow(frame string) string {
	for _, line := range lines(frame) {
		if !strings.HasPrefix(line, "›") {
			continue
		}

		_, row, _ := strings.Cut(ansi.Truncate(line, sidebarWidth, ""), "│ ")

		return strings.TrimRight(row, " ")
	}

	return ""
}

func recordLine(frame, label string) int {
	for i, line := range lines(frame) {
		if strings.HasPrefix(strings.TrimLeft(line, "› "), label+" ") {
			return i
		}
	}

	return -1
}

func legend(frame string) string {
	rows := lines(frame)
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.HasPrefix(rows[i], "─") {
			return strings.Join(rows[i+1:], "\n")
		}
	}

	return ""
}
