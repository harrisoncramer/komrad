package ui

import (
	"fmt"

	"weave/internal/ui/paint"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewPanStep is the smallest sideways jump, used until the pane has been measured.
const reviewPanStep = 8

// reviewScrollStep is how far a scroll key moves, kept small so the code under the cursor stays
// in view.
const reviewScrollStep = 15

// reviewScrollMargin is how close to the edge of the pane the cursor gets before the diff
// scrolls under it.
const reviewScrollMargin = 4

func (m model) updateReview(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.review.composing {
		return m.updateReviewComposer(key)
	}

	if m.review.releasing {
		return m.updateReviewRelease(key)
	}

	if m.review.filter.typing {
		return m.updateReviewFilter(key)
	}

	k := key.String()

	// The diff opens for an agent that is not there, and only the back key answers until it has
	// something to draw.
	if m.review.view == nil {
		if isBound(k, m.keys.Review.Back) {
			return m.leaveReview()
		}

		return m, nil
	}

	m.review.notice = ""

	// The conversation takes the keys ahead of the diff, which is otherwise moving under it on the
	// same keys.
	if next, handled := m.updateReviewThread(k); handled {
		return next, nil
	}

	return reviewActions().dispatch(m, k)
}

// updateReviewThread is the conversation answering the keys while it holds them, reporting
// whether it took the press so the diff underneath gets whatever it left alone.
func (m model) updateReviewThread(k string) (model, bool) {
	if !m.isReviewThreadFocused() {
		return m, false
	}

	switch {
	case isBound(k, m.keys.Review.FocusThread), k == "esc":
		return m.blurReviewThread(), true
	case isBound(k, m.keys.Nav.Up):
		return m.scrollReviewThread(1), true
	case isBound(k, m.keys.Nav.Down):
		return m.scrollReviewThread(-1), true
	case isBound(k, m.keys.Nav.Top):
		return m.scrollReviewThread(m.reviewThreadMax()), true
	case isBound(k, m.keys.Nav.Bottom):
		return m.scrollReviewThread(-m.review.threadScroll), true
	}

	return m, false
}

// isReviewThreadFocused is whether the sidebar holds the keys, which it does mid-input and
// while the conversation has been taken.
func (m model) isReviewThreadFocused() bool {
	if m.screen != screenReview {
		return false
	}

	if m.review.composing || m.review.releasing {
		return true
	}

	if !m.review.threadFocus || m.isDetailsFocused() {
		return false
	}

	_, ok := m.reviewThreadAtCursor()

	return ok
}

// isReviewDiffFocused is whether the patch holds the keys, which it does whenever the record and
// the conversation beside it do not.
func (m model) isReviewDiffFocused() bool {
	return m.isColumnFocused()
}

// focusReviewThread moves the keys onto the conversation, refused while there is none to read.
func (m model) focusReviewThread() model {
	if _, ok := m.reviewThreadAtCursor(); !ok {
		m.review.notice = "no thread here to read"

		return m
	}

	m.review.threadFocus = true

	return m
}

// blurReviewThread hands the keys back to the diff and drops the reader back to the end of the
// conversation.
func (m model) blurReviewThread() model {
	m.review.threadFocus = false
	m.review.threadScroll = 0

	return m
}

// scrollReviewThread moves the sidebar's window up the conversation, counted from its end so the
// newest message stays where it is as the thread grows.
func (m model) scrollReviewThread(delta int) model {
	m.review.threadScroll = max(min(m.review.threadScroll+delta, m.reviewThreadMax()), 0)

	return m
}

// reviewThreadMax is how far up the conversation the sidebar can be scrolled.
func (m model) reviewThreadMax() int {
	help, record := m.renderHelp(m.reviewHelp()), m.detailsView()
	lines := m.listLines(help, record, m.reviewHeadView(reviewStripRoom(m.listLines(help, record))))

	return max(paint.CountLines(m.reviewRestView())-max(lines-1, 1), 0)
}

// refuseReviewWrite says why a key that would write to the agent did nothing.
func (m model) refuseReviewWrite() (tea.Model, tea.Cmd) {
	reason, _ := m.isReviewReadOnly()
	m.review.notice = reason

	return m, nil
}

// refuseReviewHistory says why a write did not happen on a round the agent has moved past, and
// names the key back to the work a comment could still ask it to change.
func (m model) refuseReviewHistory(said string) (tea.Model, tea.Cmd) {
	summary := said + " is finished"
	detail := "This draws code the agent has already rewritten, so the line under the cursor is " +
		"not in the work it has now and a comment on it could not be acted on. Press " +
		keyLabel(m.keys.Review.BaseForward) + " until the whole change is on screen, then comment there."

	m.reportBrief(summary, detail, nil)
	m.review.notice = summary + ", so nothing can be written on it"

	return m, nil
}

// refuseReviewViewed says why the fold key did nothing on a chapter, and names the key back to
// the change a mark is about.
func (m model) refuseReviewViewed() (tea.Model, tea.Cmd) {
	summary := "a file is read in the whole change, not in one round"
	detail := "This draws one round of the change, so the file under the cursor is a different " +
		"diff here than it is in the whole of it and a mark taken on it would say you had read " +
		"something else. Press " + keyLabel(m.keys.Review.BaseForward) + " until the whole change " +
		"is on screen, then mark it there."

	m.reportBrief(summary, detail, nil)
	m.review.notice = summary

	return m, nil
}

// refuseReviewAwaiting says why a write did not happen while the review already sent is still
// unanswered, and names both ways out of it.
func (m model) refuseReviewAwaiting() (tea.Model, tea.Cmd) {
	summary := reviewOwedSaid(m.reviewOwedComments())
	detail := "A review is finished when its comments are settled by whoever wrote them. " +
		keyLabel(m.keys.Review.Resolve) + " settles the one under the cursor, " +
		keyLabel(m.keys.Review.Compose) + " replies in it, " +
		keyLabel(m.keys.Review.NextOpenThread) + " walks them."

	m.reportBrief(summary, detail, nil)
	m.review.notice = summary

	return m, nil
}

// updateReviewFilter is the search prompt taking keys, which is the panel's filter input drawn
// in the review's own sidebar.
func (m model) updateReviewFilter(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, m.hide()
	}

	if !m.review.filter.takeKey(key) {
		return m, nil
	}

	m.searchReview()

	return m, nil
}

// searchReview runs the pattern over the diff and moves to the first thing it finds.
func (m *model) searchReview() {
	if m.review.view == nil {
		return
	}

	found := m.review.view.SetPattern(m.review.filter.pattern)
	if found > 0 {
		if next, ok := m.review.view.JumpMatch(m.review.cursor-1, 1); ok {
			m.review.cursor = next
			m.review.pinTop = false
		}
	}

	m.scrollReviewToCursor()
}

// jumpReviewMatch steps to the next or previous line the search found, wrapping at the ends.
func (m model) jumpReviewMatch(direction int) (tea.Model, tea.Cmd) {
	next, ok := m.review.view.JumpMatch(m.review.cursor, direction)
	if !ok {
		m.review.notice = "nothing to search for yet"
		if m.review.filter.isStanding() {
			m.review.notice = "no matches"
		}

		return m, nil
	}

	return m.moveReviewCursor(next)
}

// jumpOpenThread moves to the next comment nobody has settled, wrapping to the first one, which
// is how a person clearing a review walks it.
func (m model) jumpOpenThread() (tea.Model, tea.Cmd) {
	next, ok := m.review.view.JumpOpenThread(m.review.cursor)
	if !ok {
		// Explainer: the walk wraps rather than stopping at the end, since a reader part-way down
		// a diff would otherwise be told there is nothing left while the answer is above them.
		next, ok = m.review.view.JumpOpenThread(-1)
	}

	if !ok || next == m.review.cursor {
		m.review.notice = m.noOpenThreadSaid(ok)

		return m, nil
	}

	return m.moveReviewCursor(next)
}

// noOpenThreadSaid is why the key moved nowhere, which is three different facts and never the
// same sentence.
func (m model) noOpenThreadSaid(standingOnIt bool) string {
	if standingOnIt && m.review.view.CountOpenThreads() == 1 {
		return "this is the only comment left open"
	}

	// Explainer: a thread in a file the reader has folded away is never pinned to a row, so the
	// walk cannot reach it while the count still holds it. Saying everything is settled there
	// would contradict the legend, which is counting the same thread.
	if m.review.view.CountOpenThreads() > 0 {
		return "the comments left open are in files you have folded away"
	}

	return "every comment here has been settled"
}

// threadStepDesc names the three keys that walk the conversations, by what is left to settle.
func (m model) threadStepDesc() string {
	if m.review.view == nil {
		return "thread, next open"
	}

	open := m.review.view.CountOpenThreads()
	if open == 0 {
		return "thread, next open"
	}

	return fmt.Sprintf("thread, %d open", open)
}

// reviewThreadAtCursor is the thread the cursor is standing on, and false while the diff is
// still loading.
func (m model) reviewThreadAtCursor() (*agentv1.ReviewThread, bool) {
	if m.review.view == nil {
		return nil, false
	}

	return m.review.view.GetThreadAt(m.review.cursor)
}

// toggleReviewExpanded draws the whole of the file the cursor is in, or gives back the changed
// lines and the few around them.
func (m model) toggleReviewExpanded() (tea.Model, tea.Cmd) {
	file, ok := m.review.view.GetDrawnFileAt(m.review.cursor)
	if !ok {
		return m, nil
	}

	path := file.Path()
	expanding := !m.review.view.IsExpanded(path)

	if expanding && m.review.view.IsViewed(path) {
		m.review.notice = "that file is folded away · " +
			keyLabel(m.keys.Review.ToggleViewed) + " to open it"

		return m, nil
	}

	// Where the cursor is standing is taken before the rows are built again.
	place, located := m.review.view.GetPlace(m.review.cursor)

	if m.review.view.SetFileExpanded(path, expanding) != expanding {
		m.review.notice = "there is no more of that file to show"

		return m, nil
	}

	m.review.expanded = withExpanded(m.review.expanded, path, expanding)

	m = m.blurReviewThread()
	m.keepReviewCursor(place, located)
	m.scrollReviewToCursor()

	m.review.notice = "showing the changed lines"
	if expanding {
		m.review.notice = "showing the whole file"
	}

	return m, nil
}

// withExpanded records that a file is drawn whole, or that it is not, on the set the screen
// keeps across a reload.
func withExpanded(expanded map[string]bool, path string, on bool) map[string]bool {
	if expanded == nil {
		expanded = map[string]bool{}
	}

	if on {
		expanded[path] = true
	} else {
		delete(expanded, path)
	}

	return expanded
}

// jumpReviewFile moves to the next or previous file and puts that file's header at the top of
// the pane.
func (m model) jumpReviewFile(direction int) (tea.Model, tea.Cmd) {
	next := m.review.view.JumpFile(m.review.cursor, direction)
	if next == m.review.cursor {
		return m, nil
	}

	m.review.cursor = next
	m = m.blurReviewThread()
	m.review.top = max(min(m.review.view.GetFileTopRow(next), m.review.view.Len()-1), 0)
	m.review.pinTop = true
	m.review.pinFloor = m.review.top
	m.scrollReviewToCursor()

	return m, m.markReviewThreadReadCmd()
}

func (m model) moveReviewCursor(next int) (tea.Model, tea.Cmd) {
	if next == m.review.cursor {
		return m, nil
	}

	m.review.cursor = next
	m.review.pinTop = false

	// The keys and the window both belong to the conversation the cursor was standing on, so
	// neither means anything against the next one it lands on.
	m = m.blurReviewThread()
	m.scrollReviewToCursor()

	return m, m.markReviewThreadReadCmd()
}

func (m model) scrollReview(direction int) (tea.Model, tea.Cmd) {
	cursor := m.review.cursor
	for range reviewScrollStep {
		cursor = m.review.view.Move(cursor, direction)
	}

	return m.moveReviewCursor(cursor)
}

// getReviewPanStep is a third of the pane, or the smallest jump until the pane is measured.
func (m model) getReviewPanStep() int {
	split, ok := m.screenColumnSplit()
	if !ok {
		return reviewPanStep
	}

	return max(split.column/3, reviewPanStep)
}

func (m model) panReview(delta int) (tea.Model, tea.Cmd) {
	next := max(0, m.review.pan+delta)
	if next == m.review.pan {
		return m, nil
	}

	m.review.pan = next

	return m, nil
}

// scrollReviewToCursor moves the window only when the cursor is about to leave it.
func (m *model) scrollReviewToCursor() {
	split, ok := m.screenColumnSplit()
	if !ok {
		return
	}

	height := split.rows

	// A file jump has already said where the window goes, and the margin below would drag it
	// straight back off what it was asked to show.
	if m.review.pinTop {
		return
	}

	margin := min(reviewScrollMargin, max((height-1)/2, 0))

	// A file jump is only worth anything if the file stays at the top, so the margin gives up its
	// gap until the cursor walks back above the row the jump landed on.
	floor := 0
	if m.review.cursor >= m.review.pinFloor {
		floor = m.review.pinFloor
	}

	if low := m.review.cursor - margin; low < m.review.top {
		m.review.top = max(low, floor)
	}

	if high := m.review.cursor - height + 1 + margin; high > m.review.top {
		m.review.top = high
	}
}
