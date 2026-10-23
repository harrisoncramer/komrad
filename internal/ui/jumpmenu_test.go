package ui_test

import (
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
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
				author := paneltest.ArchivedAgent("author", "author", "w1")
				author.Documents = 2
				author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC))
				task := paneltest.TaskOf(author)
				task.ArchivedAt = paneltest.ArchivedAt
				harness := paneltest.OpenArchivedAgents(t, paneltest.Workflow("w1", "komrad"), author, task)
				answerDocuments(harness, buildAuthorDocuments())
				harness.Press("j", "j", "space")
				require.Contains(t, harness.Frame(), "  author")
				harness.Press("J", "d")
				harness.Advance(time.Second)

				return harness
			},
			jump: "t",
		},
		{
			name: "the jump menu lands on a deleted task among the deleted agents",
			open: func(t *testing.T) *paneltest.Panel {
				author := paneltest.ArchivedAgent("author", "author", "w1")
				author.Documents = 2
				author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC))
				deleted := paneltest.TaskOf(author)
				deleted.ArchivedAt = paneltest.ArchivedAt
				deleted.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED
				harness := paneltest.Start(t, paneltest.Params{})
				harness.Press("F", "shift+tab", "esc")
				harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "komrad"), author, deleted))
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

// buildSubpagesAgents is a workflow whose orchestrator and one agent have written, beside an agent
// that has not and a reviewer of the one that has.
func buildSubpagesAgents() []proto.Message {
	written := timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC))
	orchestrator := paneltest.Orchestrator()
	orchestrator.Documents = 2
	orchestrator.DocumentsUpdatedAt = written

	author := paneltest.Agent("author", "author", "w1")
	author.Documents = 2
	author.DocumentsUpdatedAt = written
	author.Cwd = "/trees/author"
	quiet := paneltest.Agent("quiet", "quiet", "w1")

	return []proto.Message{
		paneltest.Workflow("w1", "komrad"),
		orchestrator,
		author,
		paneltest.TaskOf(author),
		quiet,
		paneltest.TaskOf(quiet),
		paneltest.Reviewer("reviewer", "reviewer", "author"),
	}
}

// buildAuthorDocuments is the two documents the author wrote on its task, newest first.
func buildAuthorDocuments() []*agentv1.Document {
	return []*agentv1.Document{
		{
			Id:        "d1",
			AgentId:   "author",
			TaskId:    xptr.Ptr("task-author"),
			Name:      "Design the cache",
			Type:      "RFC",
			Version:   3,
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)),
		},
		{
			Id:        "d2",
			AgentId:   "author",
			TaskId:    xptr.Ptr("task-author"),
			Name:      "Rollout report",
			Type:      "report",
			Version:   5,
			UpdatedAt: timestamppb.New(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)),
		},
	}
}

// answerDocuments lists the documents and reads the first of them whichever one is asked for.
func answerDocuments(harness *paneltest.Panel, documents []*agentv1.Document) {
	read := proto.CloneOf(documents[0])
	read.Body = "# Design the cache\n\nKeep the cache warm."

	harness.Answer(agentv1connect.AgentServiceListDocumentsProcedure, paneltest.AnswerWith(&agentv1.ListDocumentsResponse{Documents: documents}))
	harness.Answer(agentv1connect.AgentServiceGetDocumentProcedure, paneltest.AnswerWith(&agentv1.GetDocumentResponse{Document: read}))
}
