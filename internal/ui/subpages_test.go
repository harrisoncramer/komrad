package ui_test

import (
	"testing"
	"time"

	"komrad/internal/gittest"
	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	paginationv1 "komrad/proto/gen/pagination/v1"
	taskv1 "komrad/proto/gen/task/v1"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestSubpages covers the menu of an agent's subpages opened beside the agents list.
func TestSubpages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		row        string
		wantTitle  string
		wantCrumb  string
		wantCursor string
	}{
		{
			name:       "an agent's row starts on logs",
			row:        "reviewer",
			wantTitle:  "  reviewer",
			wantCrumb:  "agents › reviewer",
			wantCursor: "› logs",
		},
		{
			name:       "a task with nothing to review starts on logs",
			row:        "task-quiet",
			wantTitle:  "  task-quiet",
			wantCrumb:  "agents › task-quiet",
			wantCursor: "› logs",
		},
		{
			name:       "a workflow's row opens its orchestrator's under the workflow's name",
			row:        "komrad",
			wantTitle:  "  komrad",
			wantCrumb:  "agents › komrad",
			wantCursor: "› logs",
		},
		{
			name:       "a task with a checkout starts on review",
			row:        "task-author",
			wantTitle:  "  task-author",
			wantCrumb:  "agents › task-author",
			wantCursor: "› review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openSubpages(t, tt.row)

			frame := harness.Frame()
			assert.Contains(t, frame, tt.wantTitle)
			assert.Contains(t, frame, tt.wantCrumb)
			assert.Contains(t, frame, tt.wantCursor)
		})
	}

	t.Run("an entry with nothing behind it is gray", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "task-quiet")

		frame := harness.RawFrame()
		assert.Regexp(t, paneltest.OffEntry("review", "r"), frame)
		assert.NotRegexp(t, paneltest.OffEntry("logs", "l"), frame)
	})

	t.Run("an orchestrator has no review", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "komrad")

		frame := paneltest.StageText(harness.Frame())
		assert.NotContains(t, frame, "review")
		assert.Contains(t, frame, "terminal")
	})

	t.Run("review answers its letter", func(t *testing.T) {
		t.Parallel()

		harness, reviewed := startReviewed(t, paneltest.Params{})
		harness.Watch(reviewed.snapshot())
		harness.Press("space")
		menuUp := len(harness.Tmux().Zooms())
		require.True(t, harness.Tmux().IsZoomed())

		harness.Press("r")

		assert.Equal(t, screen.Review, paneltest.Screen(harness.Frame()))
		assert.NotContains(t, harness.Tmux().Zooms()[menuUp:], false, "the list is not rendered again on the way")
	})

	t.Run("a task's row opens the review of its task", func(t *testing.T) {
		t.Parallel()

		harness, reviewed := startReviewed(t, paneltest.Params{})
		reviewed.task.Slug = "login-fix"
		harness.Watch(reviewed.snapshot())
		require.Contains(t, paneltest.HoveredRow(harness.Frame()), "login-fix")

		harness.Press("space", "r")

		assert.Equal(t, screen.Review, paneltest.Screen(harness.Frame()))
		harness.AssertSentTo(agentv1connect.AgentServiceOpenReviewHistoryProcedure, &agentv1.OpenReviewHistoryRequest{Task: &agentv1.OpenReviewHistoryRequest_TaskId{TaskId: "t1"}})
	})

	t.Run("an agent whose worktree was removed in a workflow with no directory is not opened", func(t *testing.T) {
		t.Parallel()

		harness, reviewed := startReviewed(t, paneltest.Params{})
		reviewed.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		reviewed.task.Snapshots = 1
		harness.Watch(reviewed.snapshot())

		harness.Press("space", "r")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))
	})

	t.Run("an agent whose worktree was removed with no snapshot is not opened", func(t *testing.T) {
		t.Parallel()

		harness, reviewed := startReviewed(t, paneltest.Params{})
		reviewed.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		reviewed.workflow.Cwd = reviewed.agent.GetCwd()
		harness.Watch(reviewed.snapshot())

		harness.Press("space", "r")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))
	})

	t.Run("review on a task with no checkout says why", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "task-quiet")
		harness.Press("r")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, harness.ReadMessages(), "this agent has no checkout of its own to read")
	})

	t.Run("esc closes the menu and leaves the trail on the row", func(t *testing.T) {
		t.Parallel()

		harness := openSubpages(t, "task-author")
		harness.Press("esc")

		frame := harness.Frame()
		assert.NotContains(t, frame, "› review")
		assert.Equal(t, "agents › task-author", paneltest.Trail(frame))
	})

	t.Run("the legend offers it", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
		harness.Press(paneltest.WidenListPresses...)
		harness.MoveToRow("task-quiet")
		harness.Press("?")

		assert.Regexp(t, `space +menu`, paneltest.Legend(harness.Frame()))
	})
}

// TestSubpagesInArchive covers the menu opened beside the archive, and the way back to it.
func TestSubpagesInArchive(t *testing.T) {
	t.Parallel()

	t.Run("an archived agent has no review", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedSubpages(t)

		frame := harness.Frame()
		assert.Contains(t, frame, "agents › author")
		assert.Contains(t, frame, "› logs")
		assert.NotContains(t, paneltest.StageText(frame), "review")
		assert.Regexp(t, paneltest.OffEntry("terminal", "t"), harness.RawFrame())

		harness.Press("r")

		assert.Contains(t, harness.Frame(), "› logs")
		assert.NotContains(t, harness.ReadMessages(), "Could not open")
	})

	t.Run("git holds rebase gray, since an archived worktree is not touched", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedSubpages(t)
		harness.Press("g")

		assert.Regexp(t, paneltest.OffEntry("rebase with base", "r"), harness.RawFrame())

		harness.Press("r")

		assert.Contains(t, harness.ReadMessages(), "komrad leaves an archived task's worktree alone")
	})

	t.Run("an archived agent on a live task has no git, since the checkout is not its own", func(t *testing.T) {
		t.Parallel()

		author := newArchivedAgent("author", "author", "w1")
		harness := openArchivedAgents(t, paneltest.Workflow("w1", "komrad"), author, paneltest.TaskOf(author))
		harness.Press("j", "j", "space")
		require.Contains(t, harness.Frame(), "  author")

		stage := paneltest.StageText(harness.Frame())
		require.Contains(t, stage, "jump")
		assert.NotContains(t, stage, "git")
	})

	t.Run("documents open filtered to the archived agent", func(t *testing.T) {
		t.Parallel()

		harness := openArchivedSubpages(t)
		harness.Press("J", "d")
		harness.Advance(time.Second)

		require.Equal(t, screen.Documents, paneltest.Screen(harness.Frame()))
		assert.Equal(t, "▽ agent:author", paneltest.Search(harness.Frame()))
		harness.AssertSentTo(agentv1connect.AgentServiceListDocumentsProcedure, &agentv1.ListDocumentsRequest{
			Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](100)},
		})
	})
}

// TestStatusChecksOpen covers the status checks screen opening from the subpages menu.
func TestStatusChecksOpen(t *testing.T) {
	t.Parallel()

	t.Run("a task with no pull request has no status checks to list", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
		harness.MoveToRow("task-author")
		harness.Press("space", "x")

		frame := harness.Frame()
		assert.Equal(t, screen.StatusChecks, paneltest.Screen(harness.Frame()))
		assert.Contains(t, frame, "agents › author › status checks")
		assert.Contains(t, frame, "no pull request")
	})

	t.Run("back returns to the list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
		harness.MoveToRow("task-author")
		harness.Press("space", "x", "esc")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "author")
	})
}

// openSubpages opens the subpages menu from the agents list on the row named.
func openSubpages(t *testing.T, row string) *paneltest.Panel {
	t.Helper()

	harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
	harness.MoveToRow(row)
	harness.Press("space")

	return harness
}

// reviewed is a workflow that cuts worktrees, its writer alive in pane %9 and the writer's task,
// kept in a repository the panel can read.
type reviewed struct {
	workflow *workflowv1.Workflow
	agent    *agentv1.Agent
	task     *taskv1.Task
}

// startReviewed starts the panel over a repository of its own and answers the reads that open its
// review.
func startReviewed(t *testing.T, params paneltest.Params) (*paneltest.Panel, *reviewed) {
	t.Helper()

	repo := gittest.NewRepo(t.TempDir())
	params.Git = repo
	harness := paneltest.Start(t, params)
	harness.AnswerWorktreeReads()
	harness.Answer(agentv1connect.AgentServiceOpenReviewHistoryProcedure, paneltest.AnswerWith(&agentv1.OpenReviewHistoryResponse{
		Snapshot: &agentv1.ReviewSnapshot{Id: "snapshot-1", TaskId: "t1", Ordinal: 1, BaseSha: gittest.BaseSHA, Sha: gittest.SnapshotSHA},
		Resolved: &agentv1.ReviewResolvedSpan{BaseSha: gittest.BaseSHA, HeadSha: gittest.SnapshotSHA},
	}))
	harness.Answer(agentv1connect.AgentServiceListReviewViewedFilesProcedure, paneltest.AnswerWith(&agentv1.ListReviewViewedFilesResponse{}))

	return harness, &reviewed{
		workflow: &workflowv1.Workflow{
			Id:       "w1",
			Name:     "backend",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
		},
		agent: &agentv1.Agent{
			Id:         "a1",
			Name:       "auth",
			WorkflowId: "w1",
			Role:       agentv1.AgentRole_AGENT_ROLE_WRITER,
			TaskId:     xptr.Ptr("t1"),
			Cwd:        repo.Root,
			PaneId:     "%9",
			Alive:      xptr.Ptr(true),
			State:      agentv1.AgentState_AGENT_STATE_IDLE,
		},
		task: &taskv1.Task{
			Id:         "t1",
			WorkflowId: "w1",
			Slug:       "auth",
			Cwd:        xptr.Ptr(repo.Root),
		},
	}
}

// snapshot is the reviewed rows beside any others given.
func (r *reviewed) snapshot(rows ...proto.Message) *watchv1.WatchEvent {
	return paneltest.Snapshot(append([]proto.Message{r.workflow, r.agent, r.task}, rows...)...)
}

// openArchivedSubpages opens the subpages menu on an archived agent that has written documents.
func openArchivedSubpages(t *testing.T) *paneltest.Panel {
	t.Helper()

	author := newArchivedAgent("author", "author", "w1")
	author.Documents = 2
	author.DocumentsUpdatedAt = timestamppb.New(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC))
	task := paneltest.TaskOf(author)
	task.ArchivedAt = archivedAt

	harness := openArchivedAgents(t, paneltest.Workflow("w1", "komrad"), author, task)
	answerDocuments(harness, buildAuthorDocuments())
	harness.Press("j", "j", "space")
	require.Contains(t, harness.Frame(), "  author")

	return harness
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
