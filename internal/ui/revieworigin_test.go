package ui_test

import (
	"testing"

	"komrad/internal/gittest"
	"komrad/internal/ui/paneltest"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewJump covers the jump key, which renders the diff a comment was written against.
func TestReviewJump(t *testing.T) {
	t.Run("a comment adrift in a range lands on its line in the diff it was written against", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne()}
		harness := openReviewAt(t, server, paneltest.Params{Height: 50})

		harness.Press("<")
		require.Contains(t, paneltest.StageText(harness.Frame()), "snapshot 1 ▸ snapshot 2")

		harness.Press("t")
		frame := harness.Frame()
		require.Contains(t, paneltest.PopupProse(frame), "still in the change, outside what is on screen")

		harness.Press("space", "J")

		frame = harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "where the comment was read ▸ snapshot 1")
		assert.NotContains(t, paneltest.SidebarProse(frame), "outside what is on screen")
		assert.Contains(t, paneltest.PopupProse(frame), "add a second file")
		assert.Contains(t, findLine(paneltest.Stage(frame), "package greet"), "○")
		assert.Contains(t, paneltest.SidebarProse(frame), "1 file, +1 -1")
	})

	t.Run("the jump key goes back to the range and the comment the reader left", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne()}
		harness := openReviewAt(t, server, paneltest.Params{Height: 50})

		harness.Press("<", "t", "space", "J", "space", "J")

		assert.Contains(t, paneltest.PopupProse(harness.Frame()), "add a second file")

		harness.Press("k")
		assert.Contains(t, paneltest.StageText(harness.Frame()), "snapshot 1 ▸ snapshot 2")
	})

	for _, key := range []string{"<", ">"} {
		t.Run("the step key "+key+" leaves the jump for where the reader was", func(t *testing.T) {
			server := newEarlierReviewServer(t)
			server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne()}
			harness := openReviewAt(t, server, paneltest.Params{Height: 50})

			harness.Press("<", "t", "space", "J", key)

			assert.Contains(t, paneltest.PopupProse(harness.Frame()), "add a second file")

			harness.Press("k")
			assert.Contains(t, paneltest.StageText(harness.Frame()), "snapshot 1 ▸ snapshot 2")
		})
	}

	t.Run("the answer key leaves the jump for the whole change", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne(newAnswer())}
		harness := openReviewAt(t, server, paneltest.Params{Height: 50})

		harness.Press("t", "space", "J", "space", "a")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "where the task starts ▸ snapshot 2")
		assert.Contains(t, paneltest.SidebarProse(frame), "2 files, +4 -1")
	})

	t.Run("the jump key goes back to the answer it was pressed from", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne(newAnswer())}
		harness := openReviewAt(t, server, paneltest.Params{Height: 50})

		harness.Press("t", "space", "a", "t", "space", "J")
		require.Contains(t, paneltest.StageText(harness.Frame()), "where the comment was read ▸ snapshot 1")
		harness.Press("space", "J")

		frame := harness.Frame()
		assert.Contains(t, frame, "the answer to this comment")
		assert.Contains(t, frame, "1 file, +3 -0")
	})

	t.Run("a comment written on a later snapshot is rendered against the commit it was read on", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		thread := newThreadOnFirstLine(newHumanMessage("m1", "tidy the second file"))
		thread.SnapshotId = "snapshot-2"
		thread.SnapshotOrdinal = 2
		thread.BaseSha = gittest.SnapshotSHA
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, paneltest.Params{Height: 50})

		require.Contains(t, paneltest.SidebarProse(harness.Frame()), "2 files, +4 -1")
		harness.Press("t", "space", "J", "k")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "where the comment was read ▸ snapshot 2")
		assert.Contains(t, frame, "1 file, +3 -0")
	})

	t.Run("a comment whose own base is gone is rendered against its snapshot's base", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		thread := newCommentOnSnapshotOne()
		thread.BaseSha = "a-replayed-tree-gc-took"
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, paneltest.Params{Height: 50})

		harness.Press("<", "t", "space", "J")

		frame := harness.Frame()
		assert.Contains(t, paneltest.StageText(frame), "where the comment was read ▸ snapshot 1, shown against the snapshot's base")
		assert.Contains(t, paneltest.PopupProse(frame), "add a second file")
	})

	t.Run("a comment whose snapshot is gone says so and stays on the diff", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		thread := newCommentOnSnapshotOne()
		thread.SnapshotId = "snapshot-gone"
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, paneltest.Params{Height: 50})

		harness.Press("t", "space", "J")

		frame := harness.Frame()
		assert.Contains(t, readOpenMessages(harness), "the snapshot this comment was written on is gone")
		assert.Contains(t, paneltest.StageText(frame), "where the task starts ▸ snapshot 2")
	})

	t.Run("a range ending before the newest snapshot takes a reply and draws a new comment off", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnSnapshotOne()}
		harness := openReviewAt(t, server, paneltest.Params{Height: 50})

		harness.Press("<", "t", "space", "c")
		require.Contains(t, harness.Frame(), "  Reply")
		typeText(harness, "still wrong")
		harness.Press("enter")

		harness.AssertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "still wrong",
		})

		harness.Press("space", "J", "j", "space", "c")

		frame := harness.Frame()
		assert.NotContains(t, frame, "  Comment")
		assert.Contains(t, frame, "new comments go on snapshot 2")
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("the jump splits the replies the same way", func(t *testing.T) {
		harness := openReviewAt(t, newLaterRepliesServer(t), paneltest.Params{Height: 50})

		harness.Press("t", "space", "J")

		frame := harness.Frame()
		require.Contains(t, paneltest.StageText(frame), "where the comment was read ▸ snapshot 1")
		assert.Contains(t, paneltest.PopupProse(frame), laterRule)
	})
}
