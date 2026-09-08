package ui

import (
	"weave/config"
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
	keys := m.keys.Review

	if m.review.view == nil {
		if isBound(k, keys.Back) {
			return m.leaveReview()
		}

		return m, nil
	}

	m.review.notice = ""

	// The diff opens for an agent that is not there, so the keys that would write to it are
	// refused here rather than left to fail at the server.
	if _, dead := m.reviewIsReadOnly(); dead && isReviewWrite(k, m.keys) {
		return m.refuseReviewWrite()
	}

	// The conversation takes the keys ahead of the diff, since the keys it wants are the ones the
	// diff is otherwise moving under it.
	if next, handled := m.updateReviewThread(k); handled {
		return next, nil
	}

	switch {
	case isBound(k, keys.FocusThread):
		return m.focusReviewThread(), nil
	case isBound(k, keys.Back):
		return m.leaveReview()
	case isBound(k, keys.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Nav.Down):
		return m.moveReviewCursor(m.review.view.Move(m.review.cursor, 1))
	case isBound(k, m.keys.Nav.Up):
		return m.moveReviewCursor(m.review.view.Move(m.review.cursor, -1))
	case isBound(k, m.keys.Nav.Top):
		return m.moveReviewCursor(m.review.view.GetFirstRow())
	case isBound(k, m.keys.Nav.Bottom):
		return m.moveReviewCursor(m.review.view.GetLastRow())
	case isBound(k, keys.ScrollDown):
		return m.scrollReview(1)
	case isBound(k, keys.ScrollUp):
		return m.scrollReview(-1)
	case isBound(k, keys.NextFile):
		return m.jumpReviewFile(1)
	case isBound(k, keys.PrevFile):
		return m.jumpReviewFile(-1)
	case isBound(k, keys.NextThread):
		return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, 1))
	case isBound(k, keys.PrevThread):
		return m.moveReviewCursor(m.review.view.JumpThread(m.review.cursor, -1))
	case isBound(k, m.keys.Filter.Open):
		m.review.filter.typing = true
		m.review.filter.setQuery("")
		m.searchReview()

		return m, nil
	case isBound(k, keys.NextMatch):
		return m.jumpReviewMatch(1)
	case isBound(k, keys.PrevMatch):
		return m.jumpReviewMatch(-1)
	case isBound(k, keys.PanRight):
		return m.panReview(m.reviewPanStep())
	case isBound(k, keys.PanLeft):
		return m.panReview(-m.reviewPanStep())
	case isBound(k, keys.PanReset):
		return m.panReview(-m.review.pan)
	case isBound(k, keys.Reload):
		return m.reloadReview()
	case isBound(k, keys.EditFile):
		return m.editReviewFile()
	case isBound(k, keys.Compose):
		return m.composeReview()
	case isBound(k, keys.Resolve):
		return m.resolveReviewThread()
	case isBound(k, keys.ToggleViewed):
		return m.toggleReviewViewed()
	case isBound(k, m.keys.Form.Submit):
		return m.openReviewRelease()
	}

	return m, nil
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
// cannot while the record under it does, since both are read with the same keys.
func (m model) isReviewThreadFocused() bool {
	if !m.review.threadFocus || m.screen != screenReview || m.isDetailsFocused() {
		return false
	}

	_, ok := m.reviewThreadAtCursor()

	return ok
}

// focusReviewThread moves the keys onto the conversation, refused while there is none to read,
// since the reader would then be holding keys that moved nothing.
func (m model) focusReviewThread() model {
	if _, ok := m.reviewThreadAtCursor(); !ok {
		m.review.notice = "no thread here to read"

		return m
	}

	m.review.threadFocus = true

	return m
}

// blurReviewThread hands the keys back to the diff and drops the reader back to the end of the
// conversation, since that is where a thread stands whenever it is not being read.
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
	lines := m.listLines(m.renderHelp(m.reviewHelp()), m.detailsView(), m.reviewHeadView())

	return max(countLines(m.reviewRestView())-max(lines-1, 1), 0)
}

// refuseReviewWrite says why a key that would write to the agent did nothing, since the key is
// dropped from the help but a rebinding or a habit can still press it.
func (m model) refuseReviewWrite() (tea.Model, tea.Cmd) {
	reason, _ := m.reviewIsReadOnly()
	m.review.notice = reason

	return m, nil
}

// isReviewWrite reports whether a key would put something in front of the agent, which is the
// set that stands down on an agent with nobody there to read it, leaving out settling, deleting
// and marking a file read, since those are the person's own records and send nothing.
func isReviewWrite(k string, keys config.Keybindings) bool {
	return isBound(k, keys.Review.Compose) ||
		isBound(k, keys.Form.Submit)
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
// still loading, since the sidebar is drawn on frames that arrive before the patch does.
func (m model) reviewThreadAtCursor() (*agentv1.ReviewThread, bool) {
	if m.review.view == nil {
		return nil, false
	}

	return m.review.view.GetThreadAt(m.review.cursor)
}

// jumpReviewFile moves to the next or previous file and puts that file's header at the top of the
// pane, since the scroll margin would otherwise land a forward jump on the last rows of the window
// and show almost nothing of the file it went to.
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

// reviewPanStep is how far sideways a press goes, a third of the pane, since a fixed few
// characters meant forty presses to cross a wide diff.
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

// scrollReviewToCursor moves the window only when the cursor is about to leave it, since
// scrolling on every move repaints every row of the pane and reads as a flicker.
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
