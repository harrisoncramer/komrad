package ui

import (
	"slices"

	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewPanStep is the smallest sideways jump, used until the pane has been measured.
const reviewPanStep = 8

// reviewScrollStep is how far a scroll key moves, kept small so the code under the cursor stays
// in view rather than a whole screen going by at once.
const reviewScrollStep = 15

// reviewScrollMargin is how close to the edge of the pane the cursor gets before the diff
// scrolls under it, so there is always some of what is coming into view.
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

// isReviewThreadFocused is whether the conversation in the sidebar holds the keys, which it
// cannot while the record under it does.
func (m model) isReviewThreadFocused() bool {
	if !m.review.threadFocus || m.screen != screenReview || m.isDetailsFocused() {
		return false
	}

	_, ok := m.reviewThreadAtCursor()

	return ok
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

// reviewThreadMax is how far up the conversation the sidebar can be scrolled, which is whatever
// it holds beyond the rows the sidebar has for it.
func (m model) reviewThreadMax() int {
	lines := m.spareLines(slices.Concat(m.reviewAbove(),
		[]string{m.renderHelp(m.reviewHelp()), m.detailsView(), m.reviewHeadView()})...)

	// A sidebar with no room draws no conversation at all, so there is nothing to scroll through
	// rather than the whole of it to scroll through.
	if lines <= 0 {
		return 0
	}

	return max(countLines(m.reviewRestView())-max(lines-1, 1), 0)
}

// refuseReviewWrite says why a key that would write to the agent did nothing.
func (m model) refuseReviewWrite() (tea.Model, tea.Cmd) {
	reason, _ := m.reviewIsReadOnly()
	m.review.notice = reason

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

// searchReview runs the pattern over the diff and moves to the first thing it finds, which is
// what makes typing one worth watching.
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

// reviewThreadAtCursor is the thread the cursor is standing on, and false while the diff is
// still loading.
func (m model) reviewThreadAtCursor() (*agentv1.ReviewThread, bool) {
	if m.review.view == nil {
		return nil, false
	}

	return m.review.view.GetThreadAt(m.review.cursor)
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

// reviewPanStep is how far sideways a press goes, which is a third of the pane.
func (m model) reviewPanStep() int {
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
