package ui_test

import (
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestWorktreesJump covers jumping from a directory to the task it belongs to.
func TestWorktreesJump(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		row         string
		wantScreen  screen.ID
		wantHovered string
		wantTaskID  string
	}{
		{
			name:        "a directory nothing answers for stays put",
			row:         "task/orphan",
			wantScreen:  screen.Worktrees,
			wantHovered: "task/orphan",
		},
		{
			name:        "a live task's directory lands on the agents list",
			row:         "treeview",
			wantScreen:  screen.Agents,
			wantHovered: "treeview",
			wantTaskID:  "live",
		},
		{
			name:        "an archived task's directory lands in the archive",
			row:         "reviewer",
			wantScreen:  screen.Agents,
			wantHovered: "reviewer",
			wantTaskID:  "archived",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := paneltest.OpenAgents(t, buildWorktreeRows()...)
			harness.AnswerWorktreeReads()
			harness.Press("shift+tab", "esc")
			harness.MoveToRow(tt.row)

			harness.Press("space", "J", "t")

			frame := harness.Frame()
			assert.Equal(t, tt.wantScreen, paneltest.Screen(frame))
			assert.Contains(t, paneltest.HoveredRow(frame), tt.wantHovered)
			assert.Equal(t, tt.wantTaskID, paneltest.RecordField(frame, "task_id"))
		})
	}
}

// TestDocumentsJump covers jumping from a document to the task or the agent it belongs to.
func TestDocumentsJump(t *testing.T) {
	t.Parallel()

	openEvery := func(t *testing.T, documents []*agentv1.Document) *paneltest.Panel {
		t.Helper()

		harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
		answerDocuments(harness, documents)
		harness.MoveToRow("task-quiet")
		harness.Press("tab", "tab")
		harness.Advance(time.Second)

		return harness
	}

	tests := []struct {
		name string
		open func(t *testing.T) *paneltest.Panel
		jump string
	}{
		{
			name: "the jump menu lands on the task the hovered document belongs to",
			open: func(t *testing.T) *paneltest.Panel { return openEvery(t, buildAuthorDocuments()) },
			jump: "t",
		},
		{
			name: "the jump menu on a document with no task lands on its author",
			open: func(t *testing.T) *paneltest.Panel {
				documents := buildAuthorDocuments()
				for _, document := range documents {
					document.TaskId = nil
				}

				return openEvery(t, documents)
			},
			jump: "a",
		},
		{
			name: "the jump menu lands on an archived task among the archived agents",
			open: func(t *testing.T) *paneltest.Panel {
				harness := openArchivedSubpages(t)
				harness.Press("J", "d")
				harness.Advance(time.Second)

				return harness
			},
			jump: "t",
		},
		{
			name: "the jump menu lands on a deleted task among the deleted agents",
			open: func(t *testing.T) *paneltest.Panel {
				author := newArchivedAgent("author", "author", "w1")
				author.Documents = 2
				author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC))
				harness := paneltest.Start(t, paneltest.Params{})
				harness.Press("F", "shift+tab", "esc")
				harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "komrad"), author, newDeletedTaskOf(author)))
				answerDocuments(harness, buildAuthorDocuments())
				harness.Press("j", "j", "space", "J", "d")
				harness.Advance(time.Second)

				return harness
			},
			jump: "t",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := tt.open(t)
			harness.Press("space", "J", tt.jump)

			assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
			assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "task-author")
		})
	}
}
