package ui_test

import (
	"slices"
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	t.Run("the answer key draws the change the answer carried", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(newAnswer())}
		harness := openReviewAt(t, server, panelHarnessParams{height: 60})

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

	t.Run("the answer key leaves out what earlier answers changed since the comment's round", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.rounds = append(server.rounds, &agentv1.ReviewRound{
			Id:          "round-2",
			AgentId:     "a1",
			Ordinal:     2,
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: gittest.NameSnapshot(server.repo.Tree),
		})
		server.repo.Wrote(gittest.WroteParams{
			Tree:  "tree-with-a-tall-file",
			Whole: gittest.PatchOfBothFiles + gittest.PatchOfATallFile,
			Since: gittest.PatchOfATallFile,
		})
		server.repo.Patches[gittest.NamePair(gittest.SnapshotSHA, gittest.NameSnapshot(server.repo.Tree))] =
			gittest.PatchOfTheSecondFile + gittest.PatchOfATallFile
		server.rounds = append(server.rounds, &agentv1.ReviewRound{
			Id:          "round-3",
			AgentId:     "a1",
			Ordinal:     3,
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: gittest.NameSnapshot(server.repo.Tree),
		})
		answer := newAnswer()
		answer.RoundId = xptr.Ptr("round-3")
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(answer)}
		harness := openReviewAt(t, server, panelHarnessParams{height: 60})

		harness.press("t", "a")

		frame := readStageText(harness.readFrame())
		assert.Contains(t, frame, "the answer to this comment")
		assert.Contains(t, frame, "tall.go")
		assert.NotContains(t, frame, "second.go")
	})

	t.Run("leaving the answer puts the cursor back on the comment", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne(newAnswer())}
		harness := openReviewAt(t, server, panelHarnessParams{height: 60})

		harness.press("t", "a")
		require.Contains(t, readStageText(harness.readFrame()), "the answer to this comment")
		harness.press("j", "a")

		frame := harness.readFrame()
		require.Contains(t, readStageText(frame), "all changes")
		assert.Contains(t, readPopupProse(frame), "add a second file")
	})

	t.Run("an answer taken after the screen opened is found among the rounds listed afresh", func(t *testing.T) {
		server := newEarlierReviewServer(t)
		server.threads = []*agentv1.ReviewThread{newCommentOnRoundOne()}
		harness := openReview(t, server)

		answer := newAnswer()
		answer.RoundId = xptr.Ptr("round-3")
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
		assert.Contains(t, readOpenMessages(harness), "the answer can't be compared across the rebase, conflicts in greet.go, second.go")
		assert.Contains(t, readStageText(frame), "all changes")
		require.Contains(t, readStageText(frame), "2 files, +4 -1")
	})

	t.Run("the whole change draws no rule but still names the answer's round", func(t *testing.T) {
		harness := openReviewAt(t, newLaterRepliesServer(t), panelHarnessParams{height: 50})

		harness.press("t")

		said := readPopupProse(harness.readFrame())
		assert.NotContains(t, said, laterRule)
		assert.Contains(t, said, "addressed in round 2")
	})

	t.Run("only the newest addressed reply names its round", func(t *testing.T) {
		server := newLaterRepliesServer(t)
		earlier := newAgentMessage("m0", "first try")
		earlier.CreatedAt = "2026-09-08T09:15:00Z"
		earlier.RoundId = xptr.Ptr("round-1")
		thread := server.threads[0]
		thread.Messages = slices.Insert(thread.Messages, 1, earlier)
		harness := openReviewAt(t, server, panelHarnessParams{height: 50})

		harness.press("t")

		said := readPopupProse(harness.readFrame())
		assert.NotContains(t, said, "addressed in round 1")
		assert.Contains(t, said, "addressed in round 2")
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
	answer.RoundId = xptr.Ptr("round-2")

	return answer
}

const laterRule = "── later, after this code changed ──"

func newLaterRepliesServer(t *testing.T) *reviewServer {
	t.Helper()

	server := newEarlierReviewServer(t)
	server.rounds[0].CreatedAt = "2026-09-08T09:00:00Z"
	server.rounds = append(server.rounds, &agentv1.ReviewRound{
		Id:          "round-2",
		AgentId:     "a1",
		Ordinal:     2,
		BaseSha:     gittest.BaseSHA,
		SnapshotSha: gittest.NameSnapshot(server.repo.Tree),
		CreatedAt:   "2026-09-08T10:30:00Z",
	})

	comment := newHumanMessage("m1", "add a second file")
	comment.CreatedAt = "2026-09-08T09:10:00Z"
	looking := newAgentMessage("m2", "looking")
	looking.CreatedAt = "2026-09-08T09:20:00Z"
	answer := newAgentMessage("m3", "added it")
	answer.CreatedAt = "2026-09-08T10:40:00Z"
	answer.RoundId = xptr.Ptr("round-2")
	thanks := newHumanMessage("m4", "thanks")
	thanks.CreatedAt = "2026-09-08T10:50:00Z"

	thread := newThreadOnFirstLine(comment, looking, answer, thanks)
	thread.RoundId = "round-1"
	thread.RoundOrdinal = 1
	server.threads = []*agentv1.ReviewThread{thread}

	return server
}
