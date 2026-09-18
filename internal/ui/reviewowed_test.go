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

// agentReply is the agent answering in a thread without settling it.
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

// TestTheOwedListNamesWhatIsHoldingTheReview covers the whole point of it: what a reader had to walk
// the diff to learn is drawn once, above the conversation.
func TestTheOwedListNamesWhatIsHoldingTheReview(t *testing.T) {
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

// TestTheOwedListCountsItsOwnRows pins the number to the comments drawn and not to the flag.
func TestTheOwedListCountsItsOwnRows(t *testing.T) {
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

// TestTheOwedListGoesWhenTheLastCommentIsSettled is the same invariant at zero, where a count over
// an empty list would be the contradiction the list exists to end.
func TestTheOwedListGoesWhenTheLastCommentIsSettled(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", "a.txt", 2)})

	require.Contains(t, ansi.Strip(m.reviewSidebarView()), "Open in your review")

	m.review.threads[0].ResolvedAt = "2026-09-13T11:00:00Z"
	m.applyReviewThreads(m.review.threads)

	require.True(t, m.review.isOwedAnAnswer(), "the flag has not been asked again yet")
	assert.NotContains(t, ansi.Strip(m.reviewSidebarView()), "Open in your review")
}

// TestTheOwedListCountsWhatItCannotDraw covers a sidebar with more comments than rows.
func TestTheOwedListCountsWhatItCannotDraw(t *testing.T) {
	threads := make([]*agentv1.ReviewThread, 0, 6)
	for i := range 6 {
		threads = append(threads, owedThread(fmt.Sprintf("t%d", i), fmt.Sprintf("f%d.txt", i), int32(i+1)))
	}

	m := owedModel(t, threads)

	shown := ansi.Strip(m.reviewSidebarView())

	assert.Contains(t, shown, "Open in your review  6")
	assert.Contains(t, shown, "+ 3 more", "three are drawn and the rest are counted")
}

// TestTheOwedListIsNotDrawnOverADeadAgent covers the agent the service lifts the gate for, where a
// list saying the review is held would be false.
func TestTheOwedListIsNotDrawnOverADeadAgent(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", "a.txt", 2)})

	require.Contains(t, ansi.Strip(m.reviewSidebarView()), "Open in your review")

	m.listing.agents[0].alive = false

	assert.NotContains(t, ansi.Strip(m.reviewSidebarView()), "Open in your review")
}

// TestTheOwedListSaysWhenACommentIsFoldedAway covers the comment in a file the reader has put away,
// which the walk cannot reach and the list would otherwise name without saying why.
func TestTheOwedListSaysWhenACommentIsFoldedAway(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", fakeFile, 2)})

	require.Contains(t, ansi.Strip(m.reviewSidebarView()), "no reply")

	next, _ := m.updateReview(keyMsg("v"))
	m, ok := next.(model)
	require.True(t, ok)

	assert.Contains(t, ansi.Strip(m.reviewSidebarView()), "folded away")
}

// TestTheOwedListBrightensWhatIsOnScreen covers a comment in view being drawn in the panel's own
// colour while one that is not stays dim.
func TestTheOwedListBrightensWhatIsOnScreen(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", fakeFile, 2)})

	rows := m.reviewOwedComments()
	require.Len(t, rows, 1)
	require.True(t, m.isReviewCommentNear(rows[0]), "its line is drawn beside the list")

	bright := paint.Panel.Style(reviewOwedTone).Bold(true).Render(fakeFile + ":2")
	assert.Contains(t, m.reviewSidebarView(), bright)

	// The reader scrolls past it.
	m.review.top = m.review.view.Len() + 100

	require.False(t, m.isReviewCommentNear(m.reviewOwedComments()[0]), "and now it is not")
	assert.NotContains(t, m.reviewSidebarView(), bright)
	assert.Contains(t, m.reviewSidebarView(), dimStyle.Render(fakeFile+":2"), "the row is still named, dim")
}

// walkOpen presses a key and gives back the row the cursor landed on.
func walkOpen(t *testing.T, m model, key string) (model, int) {
	t.Helper()

	next, _ := m.updateReview(keyMsg(key))
	m, ok := next.(model)
	require.True(t, ok)

	return m, m.review.cursor
}

// TestWalkingTheOpenCommentsGoesBothWays covers the backward walk reaching the comment above the
// cursor, and needs three comments to tell that from a wrap.
func TestWalkingTheOpenCommentsGoesBothWays(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{
		owedThread("t1", fakeFile, 2),
		owedThread("t2", fakeFile, 3),
		owedThread("t3", fakeFile, 4),
	})

	m, first := walkOpen(t, m, "u")
	m, second := walkOpen(t, m, "u")
	m, third := walkOpen(t, m, "u")
	require.Less(t, first, second)
	require.Less(t, second, third)

	_, back := walkOpen(t, m, "U")

	assert.Equal(t, second, back, "the one above it, not the one a wrap would have reached")
}

// TestWalkingTheOpenCommentsStopsAtTheEnds covers the walk holding still at either end.
func TestWalkingTheOpenCommentsStopsAtTheEnds(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{
		owedThread("t1", fakeFile, 2),
		owedThread("t2", fakeFile, 3),
		owedThread("t3", fakeFile, 4),
	})

	m, first := walkOpen(t, m, "u")

	m, before := walkOpen(t, m, "U")
	assert.Equal(t, first, before, "nothing is above the first one")

	m, _ = walkOpen(t, m, "u")
	m, last := walkOpen(t, m, "u")

	m, after := walkOpen(t, m, "u")
	assert.Equal(t, last, after, "and nothing is below the last")
	assert.Contains(t, m.review.notice, "comment", "the notice says what is left")
}

// TestTheLegendFollowsTheKeysOntoTheThread covers the legend naming the conversation's own keys
// once the reader has taken the keys onto it, and the diff's again once they hand them back.
func TestTheLegendFollowsTheKeysOntoTheThread(t *testing.T) {
	m := owedModel(t, []*agentv1.ReviewThread{owedThread("t1", fakeFile, 2)})
	m.helpExpanded = true

	m, _ = walkOpen(t, m, "u")
	require.False(t, m.isReviewThreadFocused(), "the diff holds the keys to start")

	diff := ansi.Strip(m.renderHelp(m.reviewHelp()))
	assert.NotContains(t, diff, "scroll the thread")

	next, _ := m.updateReview(keyMsg("ctrl+f"))
	m, ok := next.(model)
	require.True(t, ok)
	require.True(t, m.review.threadFocus, "and now the conversation does")

	thread := ansi.Strip(m.renderHelp(m.reviewHelp()))
	assert.Contains(t, thread, "scroll the thread")
	assert.Contains(t, thread, "back to the diff")
	assert.NotEqual(t, diff, thread, "which is a different legend")

	next, _ = m.updateReview(keyMsg("ctrl+f"))
	m, ok = next.(model)
	require.True(t, ok)

	assert.Equal(t, diff, ansi.Strip(m.renderHelp(m.reviewHelp())), "and the diff's legend is back")
}
