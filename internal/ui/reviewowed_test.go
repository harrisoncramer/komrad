package ui

import (
	"fmt"
	"testing"

	"weave/internal/ui/paint"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// owedThread is a comment of the reader's holding the review the gate reads.
func owedThread(id, path string, line int32, messages ...*agentv1.ReviewMessage) *agentv1.ReviewThread {
	return &agentv1.ReviewThread{
		Id:       id,
		ReviewId: "r1",
		FilePath: path,
		NewStart: line,
		Messages: append([]*agentv1.ReviewMessage{{
			Id:         id + "-m1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Body:       "this needs plain english",
			Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
		}}, messages...),
	}
}

// agentReply is the agent answering in a thread, which is what settles nothing and says a
// comment has at least been read.
func agentReply() *agentv1.ReviewMessage {
	return &agentv1.ReviewMessage{
		Id:         "m-agent",
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
		Body:       "rewrote the header",
	}
}

// owedModel is a review screen holding a gating review and the comments under it.
func owedModel(t *testing.T, threads []*agentv1.ReviewThread) model {
	t.Helper()

	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	agents.threads = threads
	agents.reviews = []*agentv1.Review{{
		Id:          "r1",
		AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:  "2026-09-13T10:00:00Z",
		OpenThreads: int32(len(threads)),
	}}

	return refreshReview(t, m)
}

// TestTheStripNamesWhatIsHoldingTheReview covers the whole point of it: what a reader had to walk
// the diff to learn is drawn once, above the conversation.
func TestTheStripNamesWhatIsHoldingTheReview(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{
		owedThread("t1", "a.txt", 2),
		owedThread("t2", "b.txt", 5, agentReply()),
	})

	shown := ansi.Strip(m.reviewSidebarView())

	assert.Contains(t, shown, "Open in your review")
	assert.Contains(t, shown, "a.txt:2")
	assert.Contains(t, shown, "no reply")
	assert.Contains(t, shown, "b.txt:5")
	assert.Contains(t, shown, "answered")
}

// TestTheStripCountsItsOwnRows pins the number to the comments it is drawing rather than to the
// flag, since the two disagree for a tick after a comment is settled.
func TestTheStripCountsItsOwnRows(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{
		owedThread("t1", "a.txt", 2),
		owedThread("t2", "b.txt", 5),
	})

	require.Contains(t, ansi.Strip(m.reviewSidebarView()), "Open in your review  2")

	m.review.threads[0].ResolvedAt = "2026-09-13T11:00:00Z"
	m.applyReviewThreads(m.review.threads)

	shown := ansi.Strip(m.reviewSidebarView())

	require.True(t, m.review.isOwedAnAnswer(), "the flag has not been asked again yet")
	assert.Contains(t, shown, "Open in your review  1")
	assert.NotContains(t, shown, "a.txt:2")
}

// TestTheStripGoesWhenTheLastCommentIsSettled is the same invariant at zero, where a count over
// an empty list would be the contradiction the strip exists to end.
func TestTheStripGoesWhenTheLastCommentIsSettled(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", "a.txt", 2)})

	require.Contains(t, ansi.Strip(m.reviewSidebarView()), "Open in your review")

	m.review.threads[0].ResolvedAt = "2026-09-13T11:00:00Z"
	m.applyReviewThreads(m.review.threads)

	require.True(t, m.review.isOwedAnAnswer(), "the flag has not been asked again yet")
	assert.NotContains(t, ansi.Strip(m.reviewSidebarView()), "Open in your review")
}

// TestTheStripCountsWhatItCannotDraw covers a sidebar with more comments than rows, which says
// how many are left rather than dropping them.
func TestTheStripCountsWhatItCannotDraw(t *testing.T) {
	threads := make([]*agentv1.ReviewThread, 0, 6)
	for i := range 6 {
		threads = append(threads, owedThread(fmt.Sprintf("t%d", i), fmt.Sprintf("f%d.txt", i), int32(i+1)))
	}

	m := owedModel(t, threads)

	shown := ansi.Strip(m.reviewSidebarView())

	assert.Contains(t, shown, "Open in your review  6")
	assert.Contains(t, shown, "+ 3 more", "three are drawn and the rest are counted")
}

// TestTheStripIsNotDrawnOverADeadAgent covers the agent the service lifts the gate for, where a
// strip saying the review is held would be false.
func TestTheStripIsNotDrawnOverADeadAgent(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", "a.txt", 2)})

	require.Contains(t, ansi.Strip(m.reviewSidebarView()), "Open in your review")

	m.listing.agents[0].alive = false

	assert.NotContains(t, ansi.Strip(m.reviewSidebarView()), "Open in your review")
}

// TestTheStripSaysWhenACommentIsFoldedAway covers the comment in a file the reader has put away,
// which the walk cannot reach and the strip would otherwise name without saying why.
func TestTheStripSaysWhenACommentIsFoldedAway(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", fakeFile, 2)})

	require.Contains(t, ansi.Strip(m.reviewSidebarView()), "no reply")

	next, _ := m.updateReview(keyMsg("v"))
	m, ok := next.(model)
	require.True(t, ok)

	assert.Contains(t, ansi.Strip(m.reviewSidebarView()), "folded away")
}

// Explainer: the strip and the diff are one view, so a comment the reader can see from where they
// are standing is drawn brighter than one they cannot. The sidebar is greyed whole while the diff
// holds the keys, so what carries that difference is the weight rather than the colour, and the
// profile has to be forced or nothing renders with either.

// TestTheStripBrightensWhatIsOnScreen covers that link.
func TestTheStripBrightensWhatIsOnScreen(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", fakeFile, 2)})

	rows := m.reviewOwedComments()
	require.Len(t, rows, 1)
	require.True(t, m.isReviewCommentNear(rows[0]), "its line is drawn beside the strip")

	bright := paint.Grey.Style(reviewOwedTone).Bold(true).Render(fakeFile + ":2")
	assert.Contains(t, m.reviewSidebarView(), bright)

	// The reader scrolls past it, which is the only thing that changes.
	m.review.top = m.review.view.Len() + 100

	require.False(t, m.isReviewCommentNear(m.reviewOwedComments()[0]), "and now it is not")
	assert.NotContains(t, m.reviewSidebarView(), bright)
}
