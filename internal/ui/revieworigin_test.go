package ui_test

import (
	"testing"

	"komrad/internal/gittest"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewJump covers the jump key, which draws the diff a comment was written against.
func TestReviewJump(t *testing.T) {
	t.Run("a comment adrift in a chapter lands on its line in the diff it was written against", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne()}
		harness := openReviewAt(t, server, panelHarnessParams{height: 50})

		harness.press("<", "t")
		frame := harness.readFrame()
		require.Contains(t, readStageText(frame), "chapter 2 of 2")
		require.Contains(t, readPopupProse(frame), "still in the change, outside what is on screen")

		harness.press("J")

		frame = harness.readFrame()
		assert.Contains(t, readStageText(frame), "where this comment was written, round 1")
		assert.NotContains(t, readStageText(frame), "outside what is on screen")
		assert.Contains(t, readPopupProse(frame), "add a second file")
		assert.Contains(t, findLine(readStage(frame), "package greet"), "○")
		assert.Contains(t, readStageText(frame), "J back")
		assert.Contains(t, readStageText(frame), "1 file, +1 -1")
	})

	t.Run("the jump key goes back to the chapter and the comment the reader left", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne()}
		harness := openReviewAt(t, server, panelHarnessParams{height: 50})

		harness.press("<", "t", "J", "J")

		frame := harness.readFrame()
		assert.Contains(t, readStageText(frame), "chapter 2 of 2")
		assert.Contains(t, readPopupProse(frame), "add a second file")
	})

	for _, key := range []string{"<", ">"} {
		t.Run("the chapter key "+key+" leaves the jump for where the reader was", func(t *testing.T) {
			server := newEarlierReviewServer(t)
			server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne()}
			harness := openReviewAt(t, server, panelHarnessParams{height: 50})

			harness.press("<", "t", "J", key)

			frame := harness.readFrame()
			assert.Contains(t, readStageText(frame), "chapter 2 of 2")
			assert.Contains(t, readPopupProse(frame), "add a second file")
		})
	}

	t.Run("the answer key leaves the jump for the whole change", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(newAnswer())}
		harness := openReviewAt(t, server, panelHarnessParams{height: 50})

		harness.press("t", "J", "a")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "all changes")
		assert.Contains(t, frame, "2 files, +4 -1")
	})

	t.Run("the jump key goes back to the answer it was pressed from", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(newAnswer())}
		harness := openReviewAt(t, server, panelHarnessParams{height: 50})

		harness.press("t", "a", "t", "J")
		require.Contains(t, readStageText(harness.readFrame()), "where this comment was written, round 1")
		harness.press("J")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "the answer to this comment")
		assert.Contains(t, frame, "1 file, +3 -0")
	})

	t.Run("a comment written on a narrowed chapter is drawn against that chapter's base", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		thread := newThreadOnFirstLine(newHumanMessage("m1", "tidy the second file"))
		thread.RoundId = "round-2"
		thread.RoundOrdinal = 2
		thread.BaseSha = gittest.SnapshotSHA
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, panelHarnessParams{height: 50})

		require.Contains(t, readStageText(harness.readFrame()), "2 files, +4 -1")
		harness.press("t", "J")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "where this comment was written, round 2")
		assert.Contains(t, frame, "1 file, +3 -0")
	})

	t.Run("a comment whose own base is gone is drawn against its round's base", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		thread := newCommentOnRoundOne()
		thread.BaseSha = "a-replayed-tree-gc-took"
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, panelHarnessParams{height: 50})

		harness.press("<", "t", "J")

		frame := harness.readFrame()
		assert.Contains(t, readStageText(frame), "where this comment was written, round 1, shown against the round's base")
		assert.Contains(t, readPopupProse(frame), "add a second file")
	})

	t.Run("a comment whose round is gone says so and stays on the diff", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		thread := newCommentOnRoundOne()
		thread.RoundId = "round-gone"
		server.threads = []*agentv1.ReviewThread{thread}
		harness := openReviewAt(t, server, panelHarnessParams{height: 50})

		harness.press("t", "J")

		frame := harness.readFrame()
		assert.Contains(t, readOpenMessages(harness), "the round this comment was written on is gone")
		assert.Contains(t, readStageText(frame), "all changes")
	})

	t.Run("a finished chapter takes a reply and refuses a new comment", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne()}
		harness := openReviewAt(t, server, panelHarnessParams{height: 50})

		harness.press("<", "t", "c")
		require.Contains(t, harness.readFrame(), "  Reply")
		typeText(harness, "still wrong")
		harness.press("ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceAddReviewMessageProcedure, &agentv1.AddReviewMessageRequest{
			ThreadId: "t1",
			Body:     "still wrong",
		})

		harness.press("J", "j", "c")

		frame := harness.readFrame()
		assert.NotContains(t, frame, "  Comment")
		assert.Contains(t, readEngineLine(frame), "is finished")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateReviewThreadProcedure))
	})

	t.Run("the jump splits the replies the same way", func(t *testing.T) {
		harness := openReviewAt(t, newLaterRepliesServer(t), panelHarnessParams{height: 50})

		harness.press("t", "J")

		frame := harness.readFrame()
		require.Contains(t, readStageText(frame), "where this comment was written, round 1")
		assert.Contains(t, readPopupProse(frame), laterRule)
	})
}
