package ui_test

import (
	"slices"
	"testing"

	"komrad/internal/gittest"
	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewAnswer covers the answer key, which renders the change an addressed reply carried.
func TestReviewAnswer(t *testing.T) {
	t.Run("a comment nobody has addressed has no answer to show", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne(newAgentMessage("m2", "why?"))}
		harness := openReview(t, server)

		harness.Press("t", "?")
		require.NotContains(t, paneltest.Legend(harness.Frame()), "the answer")
		harness.Press("?", "a")

		assert.Contains(t, paneltest.StageText(harness.Frame()), "where the task starts ▸ snapshot 2")
	})

	t.Run("the answer key renders the change the answer carried", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne(newAnswer())}
		harness := openReviewAt(t, server, paneltest.Params{Height: 60})

		harness.Press("t", "space")
		require.Contains(t, harness.Frame(), "the answer")
		harness.Press("a")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "the answer to this comment")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file, +3 -0")

		harness.Press("space", "a")

		frame = harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "where the task starts ▸ snapshot 2")
		assert.Contains(t, paneltest.SidebarProse(frame), "2 files, +4 -1")
	})

	t.Run("the answer key leaves out what earlier answers changed since the comment's snapshot", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.snapshots = append(server.snapshots, &agentv1.ReviewSnapshot{
			Id:      "snapshot-2",
			TaskId:  "t1",
			Ordinal: 2,
			BaseSha: gittest.BaseSHA,
			Sha:     gittest.NameSnapshot(server.repo.Tree),
		})
		server.repo.Wrote(gittest.WroteParams{
			Tree:  "tree-with-a-tall-file",
			Whole: gittest.PatchOfBothFiles + gittest.PatchOfATallFile,
			Since: gittest.PatchOfATallFile,
		})
		server.repo.Patches[gittest.NamePair(gittest.SnapshotSHA, gittest.NameSnapshot(server.repo.Tree))] =
			gittest.PatchOfTheSecondFile + gittest.PatchOfATallFile
		server.snapshots = append(server.snapshots, &agentv1.ReviewSnapshot{
			Id:      "snapshot-3",
			TaskId:  "t1",
			Ordinal: 3,
			BaseSha: gittest.BaseSHA,
			Sha:     gittest.NameSnapshot(server.repo.Tree),
		})
		answer := newAnswer()
		answer.SnapshotId = xptr.Ptr("snapshot-3")
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne(answer)}
		harness := openReviewAt(t, server, paneltest.Params{Height: 60})

		harness.Press("t", "space", "a")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "the answer to this comment")
		assert.Contains(t, frame, "tall.go")
		assert.NotContains(t, frame, "second.go")
	})

	t.Run("leaving the answer puts the cursor back on the comment", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne(newAnswer())}
		harness := openReviewAt(t, server, paneltest.Params{Height: 60})

		harness.Press("t", "space", "a")
		require.Contains(t, paneltest.StageText(harness.Frame()), "the answer to this comment")
		harness.Press("j", "space", "a")

		frame := harness.Frame()
		require.Contains(t, paneltest.StageText(frame), "where the task starts ▸ snapshot 2")
		assert.Contains(t, paneltest.PopupProse(frame), "add a second file")
	})

	t.Run("an answer taken after the screen opened is found among the snapshots listed afresh", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne()}
		harness := openReview(t, server)

		answer := newAnswer()
		answer.SnapshotId = xptr.Ptr("snapshot-3")
		server.snapshots = append(server.snapshots, &agentv1.ReviewSnapshot{
			Id:      "snapshot-3",
			TaskId:  "t1",
			Ordinal: 3,
			BaseSha: gittest.BaseSHA,
			Sha:     server.snapshots[len(server.snapshots)-1].GetSha(),
		})
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne(answer)}
		harness.Watch(server.buildSnapshot())

		harness.Press("t", "space", "a")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "the answer to this comment")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file, +3 -0")
	})

	t.Run("an answer across a rebase is replayed onto the answer's base", func(t *testing.T) {
		server := newMovedBaseServer(t, replayedTree)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne(newAnswer())}
		harness := openReview(t, server)

		harness.Press("t", "space", "a")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "the answer to this comment, replayed across the rebase")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file, +3 -0")
	})

	t.Run("an answer the rebase conflicts with is refused and names the paths", func(t *testing.T) {
		server := newMovedBaseServer(t, "a-tree-with-markers-in-it", gittest.File, "second.go")
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne(newAnswer())}
		harness := openReview(t, server)

		harness.Press("t", "space", "a")

		frame := harness.Frame()
		assert.Contains(t, readOpenMessages(harness), "the answer can't be compared across the rebase, conflicts in greet.go, second.go")
		assert.Contains(t, paneltest.StageText(frame), "where the task starts ▸ snapshot 2")
		require.Contains(t, paneltest.SidebarProse(frame), "2 files, +4 -1")
	})

	t.Run("the whole change renders no rule but still names the answer's snapshot", func(t *testing.T) {
		harness := openReviewAt(t, newLaterRepliesServer(t), paneltest.Params{Height: 50})

		harness.Press("t")

		said := paneltest.PopupProse(harness.Frame())
		assert.NotContains(t, said, laterRule)
		assert.Contains(t, said, "addressed in snapshot 2")
	})

	t.Run("only the newest addressed reply names its snapshot", func(t *testing.T) {
		server := newLaterRepliesServer(t)
		earlier := newAgentMessage("m0", "first try")
		earlier.CreatedAt = "2026-09-08T09:15:00Z"
		earlier.SnapshotId = xptr.Ptr("snapshot-1")
		thread := server.threads[0]
		thread.Messages = slices.Insert(thread.Messages, 1, earlier)
		harness := openReviewAt(t, server, paneltest.Params{Height: 50})

		harness.Press("t")

		said := paneltest.PopupProse(harness.Frame())
		assert.NotContains(t, said, "addressed in snapshot 1")
		assert.Contains(t, said, "addressed in snapshot 2")
	})
}

func newCommentOnSnapshotOne(replies ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	thread := newThreadOnFirstLine(append([]*agentv1.ReviewMessage{newHumanMessage("m1", "add a second file")}, replies...)...)
	thread.SnapshotId = "snapshot-1"
	thread.SnapshotOrdinal = 1

	return thread
}

func newAnswer() *agentv1.ReviewMessage {
	answer := newAgentMessage("m2", "added it")
	answer.SnapshotId = xptr.Ptr("snapshot-2")

	return answer
}

const laterRule = "── later, after this code changed ──"

func newLaterRepliesServer(t *testing.T) *reviewServer {
	t.Helper()

	server := newEarlierReviewServer(t)
	server.snapshots[0].CreatedAt = "2026-09-08T09:00:00Z"
	server.snapshots = append(server.snapshots, &agentv1.ReviewSnapshot{
		Id:        "snapshot-2",
		TaskId:    "t1",
		Ordinal:   2,
		BaseSha:   gittest.BaseSHA,
		Sha:       gittest.NameSnapshot(server.repo.Tree),
		CreatedAt: "2026-09-08T10:30:00Z",
	})

	comment := newHumanMessage("m1", "add a second file")
	comment.CreatedAt = "2026-09-08T09:10:00Z"
	looking := newAgentMessage("m2", "looking")
	looking.CreatedAt = "2026-09-08T09:20:00Z"
	answer := newAgentMessage("m3", "added it")
	answer.CreatedAt = "2026-09-08T10:40:00Z"
	answer.SnapshotId = xptr.Ptr("snapshot-2")
	thanks := newHumanMessage("m4", "thanks")
	thanks.CreatedAt = "2026-09-08T10:50:00Z"

	thread := newThreadOnFirstLine(comment, looking, answer, thanks)
	thread.SnapshotId = "snapshot-1"
	thread.SnapshotOrdinal = 1
	server.threads = []*agentv1.ReviewThread{thread}

	return server
}
