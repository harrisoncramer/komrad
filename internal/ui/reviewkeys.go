package ui

import (
	"fmt"

	"weave/internal/ui/paint"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewPanStep is the smallest sideways jump, used until the pane has been measured.
const reviewPanStep = 8

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

	if m.review.diff.filter.typing {
		return m.updateReviewFilter(key)
	}

	k := key.String()

	// The diff opens for an agent that is not there, and only the back key answers until it has
	// something to draw.
	if m.review.diff.view == nil {
		if isBound(k, m.keys.Review.Back) {
			return m.leaveReview()
		}

		return m, nil
	}

	m.review.notice = ""

	// The conversation takes the keys ahead of the diff.
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

	_, ok := m.review.diff.threadAtCursor()

	return ok
}

// isReviewDiffFocused is whether the patch holds the keys, which it does whenever the record and
// the conversation beside it do not.
func (m model) isReviewDiffFocused() bool {
	return m.isColumnFocused()
}

// focusReviewThread moves the keys onto the conversation, refused while there is none to read.
func (m model) focusReviewThread() model {
	if _, ok := m.review.diff.threadAtCursor(); !ok {
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
	lines := m.listLines(help, record, m.reviewHeadView(reviewOwedRoom(m.listLines(help, record))))

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
	detail := "You may not leave more comments until this review is settled. Reviews are settled " +
		"when all comments are addressed. Press " + keyLabel(m.keys.Review.NextOpenThread) +
		" to walk to the next unresolved comment, and reply with " +
		keyLabel(m.keys.Review.Compose) + " or press " + keyLabel(m.keys.Review.Resolve) +
		" to resolve it."

	m.reportBrief(summary, detail, nil)
	m.review.notice = summary

	return m, nil
}

// updateReviewFilter is the search prompt taking keys.
func (m model) updateReviewFilter(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "ctrl+c" {
		return m, m.hide()
	}

	if !m.review.diff.filter.takeKey(key) {
		return m, nil
	}

	m.searchReview()

	return m, nil
}

// searchReview runs the pattern over the diff and moves to the first thing it finds.
func (m *model) searchReview() {
	if m.review.diff.view == nil {
		return
	}

	m.review.diff.search()
	m.scrollReviewToCursor()
}

// search runs the pattern over the diff and puts the cursor on the first match at or after it.
func (d *reviewDiff) search() {
	found := d.view.SetPattern(d.filter.pattern)
	if found == 0 {
		return
	}

	if next, ok := d.view.JumpMatch(d.cursor-1, 1); ok {
		d.cursor = next
		d.pinTop = false
	}
}

// jumpReviewMatch steps to the next or previous line the search found, wrapping at the ends.
func (m model) jumpReviewMatch(direction int) (tea.Model, tea.Cmd) {
	next, ok := m.review.diff.view.JumpMatch(m.review.diff.cursor, direction)
	if !ok {
		m.review.notice = "nothing to search for yet"
		if m.review.diff.filter.isStanding() {
			m.review.notice = "no matches"
		}

		return m, nil
	}

	return m.moveReviewCursor(next)
}

// jumpOpenThread moves to the next comment nobody has settled, stopping at the ends.
func (m model) jumpOpenThread(direction int) (tea.Model, tea.Cmd) {
	next, ok := m.review.diff.view.JumpOpenThread(m.review.diff.cursor, direction)
	if !ok || next == m.review.diff.cursor {
		m.review.notice = m.review.diff.noOpenThreadSaid()

		return m, nil
	}

	return m.moveReviewCursor(next)
}

// noOpenThreadSaid is why the key moved nowhere.
func (d reviewDiff) noOpenThreadSaid() string {
	if d.view.IsOpenThreadAt(d.cursor) && d.view.CountOpenThreads() == 1 {
		return "this is the only comment left open"
	}

	if d.view.CountOpenThreads() > 0 {
		return "the comments left open are in files you have folded away"
	}

	return "every comment here has been settled"
}

// threadStepDesc names the three keys that walk the conversations, by what is left to settle.
func (d reviewDiff) threadStepDesc() string {
	if d.view == nil {
		return "thread, next open"
	}

	open := d.view.CountOpenThreads()
	if open == 0 {
		return "thread, next open"
	}

	return fmt.Sprintf("thread, %d open", open)
}

// threadAtCursor is the thread the cursor is standing on, and false while the diff is still
// loading.
func (d reviewDiff) threadAtCursor() (*agentv1.ReviewThread, bool) {
	if d.view == nil {
		return nil, false
	}

	return d.view.GetThreadAt(d.cursor)
}

// toggleReviewExpanded draws the whole of the file the cursor is in, or gives back the changed
// lines and the few around them.
func (m model) toggleReviewExpanded() (tea.Model, tea.Cmd) {
	file, ok := m.review.diff.view.GetDrawnFileAt(m.review.diff.cursor)
	if !ok {
		return m, nil
	}

	path := file.Path()
	expanding := !m.review.diff.view.IsExpanded(path)

	if expanding && m.review.diff.view.IsViewed(path) {
		m.review.notice = "that file is folded away · " +
			keyLabel(m.keys.Review.ToggleViewed) + " to open it"

		return m, nil
	}

	// Where the cursor is standing is taken before the rows are built again.
	place, located := m.review.diff.view.GetPlace(m.review.diff.cursor)

	if m.review.diff.view.SetFileExpanded(path, expanding) != expanding {
		m.review.notice = "there is no more of that file to show"

		return m, nil
	}

	m = m.blurReviewThread()
	m.review.diff.keepCursor(place, located)
	m.scrollReviewToCursor()

	m.review.notice = "showing the changed lines"
	if expanding {
		m.review.notice = "showing the whole file"
	}

	return m, nil
}

// jumpReviewFile moves to the next or previous file and puts that file's header at the top of
// the pane.
func (m model) jumpReviewFile(direction int) (tea.Model, tea.Cmd) {
	next := m.review.diff.view.JumpFile(m.review.diff.cursor, direction)
	if next == m.review.diff.cursor {
		return m, nil
	}

	m.review.diff.cursor = next
	m = m.blurReviewThread()
	m.review.diff.top = max(min(m.review.diff.view.GetFileTopRow(next), m.review.diff.view.Len()-1), 0)
	m.review.diff.pinTop = true
	m.review.diff.pinFloor = m.review.diff.top
	m.scrollReviewToCursor()

	return m, m.markReviewThreadReadCmd()
}

func (m model) moveReviewCursor(next int) (tea.Model, tea.Cmd) {
	if next == m.review.diff.cursor {
		return m, nil
	}

	m.review.diff.cursor = next
	m.review.diff.pinTop = false

	// The keys and the window both belong to the conversation the cursor was standing on, so
	// neither means anything against the next one it lands on.
	m = m.blurReviewThread()
	m.scrollReviewToCursor()

	return m, m.markReviewThreadReadCmd()
}

func (m model) scrollReview(direction int) (tea.Model, tea.Cmd) {
	cursor := m.review.diff.cursor
	for range navPageStep {
		cursor = m.review.diff.view.Move(cursor, direction)
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
	m.review.diff.panBy(delta)

	return m, nil
}

// panBy moves the diff sideways, stopping at its left edge.
func (d *reviewDiff) panBy(delta int) {
	d.pan = max(0, d.pan+delta)
}

// scrollReviewToCursor moves the window only when the cursor is about to leave it.
func (m *model) scrollReviewToCursor() {
	split, ok := m.screenColumnSplit()
	if !ok {
		return
	}

	m.review.diff.scrollToCursor(split.rows)
}

// scrollToCursor moves a window of the given height only when the cursor is about to leave it.
func (d *reviewDiff) scrollToCursor(height int) {
	// A file jump has already said where the window goes, and the margin below would drag it
	// straight back off what it was asked to show.
	if d.pinTop {
		return
	}

	margin := min(reviewScrollMargin, max((height-1)/2, 0))

	// A file jump is only worth anything if the file stays at the top, so the margin gives up its
	// gap until the cursor walks back above the row the jump landed on.
	floor := 0
	if d.cursor >= d.pinFloor {
		floor = d.pinFloor
	}

	if low := d.cursor - margin; low < d.top {
		d.top = max(low, floor)
	}

	if high := d.cursor - height + 1 + margin; high > d.top {
		d.top = high
	}
}
