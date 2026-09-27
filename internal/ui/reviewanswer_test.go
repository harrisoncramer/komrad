package ui_test

import (
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestReviewAnswer covers the answer key, which draws the change an addressed reply carried.
func TestReviewAnswer(t *testing.T) {
	t.Run("a comment nobody has addressed has no answer to show", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(newAgentMessage("m2", "why?"))}
		harness := openReview(t, server)

		harness.press("t", "?")
		require.NotContains(t, readLegend(harness.readFrame()), "the answer")
		harness.press("?", "a")

		assert.Contains(t, readStageText(harness.readFrame()), "all changes")
	})

	t.Run("the answer key draws the change between the comment's round and the answer's", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(newAnswer())}
		harness := openReview(t, server)

		harness.press("t", "?")
		require.Contains(t, readLegend(harness.readFrame()), "the answer")
		harness.press("?", "a")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "the answer to this comment")
		assert.Contains(t, frame, "1 file, +3 -0")

		harness.press("a")

		frame = readStageText(harness.readFrame())
		assert.Contains(t, frame, "all changes")
		assert.Contains(t, frame, "2 files, +4 -1")
	})

	t.Run("an answer taken after the screen opened is found among the rounds listed afresh", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne()}
		harness := openReview(t, server)

		answer := newAnswer()
		answer.RoundId = proto.String("round-3")
		server.rounds = append(server.rounds, &agentv1.ReviewRound{
			Id:          "round-3",
			AgentId:     "a1",
			Ordinal:     3,
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: server.rounds[len(server.rounds)-1].GetSnapshotSha(),
		})
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(answer)}
		harness.watch(server.buildSnapshot())

		harness.press("t", "a")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "the answer to this comment")
		assert.Contains(t, frame, "1 file, +3 -0")
	})

	t.Run("an answer across a rebase is replayed onto the answer's base", func(t *testing.T) {
		server := newMovedBaseServer(t, &git.Replayed{Tree: replayedTree})
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(newAnswer())}
		harness := openReview(t, server)

		harness.press("t", "a")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "the answer to this comment, replayed across the rebase")
		assert.Contains(t, frame, "1 file, +3 -0")
	})

	t.Run("an answer the rebase conflicts with is refused and names the paths", func(t *testing.T) {
		server := newMovedBaseServer(t, &git.Replayed{
			Tree:      "a-tree-with-markers-in-it",
			Conflicts: []string{gittest.File, "second.go"},
		})
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(newAnswer())}
		harness := openReview(t, server)

		harness.press("t", "a")

		frame := harness.readFrame()
		assert.Contains(t, readSidebarProse(frame), "the answer can't be compared across the rebase, conflicts in greet.go, second.go")
		assert.Contains(t, readStageText(frame), "all changes")
		require.Contains(t, readStageText(frame), "2 files, +4 -1")
	})
}

func newCommentOnRoundOne(replies ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	thread := newThreadOnFirstLine(append([]*agentv1.ReviewMessage{newHumanMessage("m1", "add a second file")}, replies...)...)
	thread.RoundId = "round-1"
	thread.RoundOrdinal = 1

	return thread
}

func newAnswer() *agentv1.ReviewMessage {
	answer := newAgentMessage("m2", "added it")
	answer.RoundId = proto.String("round-2")

	return answer
}
