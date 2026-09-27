package ui

import (
	"fmt"

	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// reviewPanStep is the smallest sideways jump, used until the pane has been measured.
const reviewPanStep = 8

// reviewScrollMargin is how close to the edge of the pane the cursor gets before the diff
// scrolls under it.
const reviewScrollMargin = 4

func (r *reviewScreen) updateKey(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	if r.threads.composing {
		return r.updateComposer(h, s, key)
	}

	if r.threads.releasing {
		return r.updateRelease(h, s, key)
	}

	if r.diff.filter.typing {
		return r.updateFilter(h, s, key)
	}

	k := key.String()

	// The diff opens for an agent that is not there, and only the back key answers until it has
	// something to draw.
	if r.diff.view == nil {
		if isBound(k, s.keys.Review.Back) {
			return r.leave(h, s)
		}

		return nil
	}

	r.notice = ""

	// The conversation takes the keys ahead of the diff.
	if r.updateThread(s, k) {
		return nil
	}

	return r.actions().dispatch(h, s, k)
}

// updateThread is the conversation answering the keys while it holds them, reporting whether it
// took the press so the diff underneath gets whatever it left alone.
func (r *reviewScreen) updateThread(s panelShared, k string) bool {
	if !r.isThreadFocused(s) {
		return false
	}

	switch {
	case k == "esc":
		r.threads.blur()

		return true
	case isBound(k, s.keys.Nav.Up):
		r.scrollThread(s, 1)

		return true
	case isBound(k, s.keys.Nav.Down):
		r.scrollThread(s, -1)

		return true
	case isBound(k, s.keys.Nav.Top):
		r.scrollThread(s, r.threadMax(s))

		return true
	case isBound(k, s.keys.Nav.Bottom):
		r.scrollThread(s, -r.threads.scroll)

		return true
	case isBound(k, s.keys.Column.ScrollUp):
		r.scrollThread(s, reviewWheelStep)

		return true
	case isBound(k, s.keys.Column.ScrollDown):
		r.scrollThread(s, -reviewWheelStep)

		return true
	}

	return false
}

// blur hands the keys back to the diff and drops the reader back to the end of the conversation.
func (t *reviewThreads) blur() {
	t.focus = false
	t.scroll = 0
}

// scrollThread moves the sidebar's window up the conversation, counted from its end so the
// newest message stays where it is as the thread grows.
func (r *reviewScreen) scrollThread(s panelShared, delta int) {
	r.threads.scroll = max(min(r.threads.scroll+delta, r.threadMax(s)), 0)
}

// threadMax is how far up the conversation the sidebar can be scrolled.
func (r reviewScreen) threadMax(s panelShared) int {
	lines := r.layoutSidebarAsDrawn(s).threadLines

	return max(len(splitConversation(r.restView(s)))-max(lines-1, 1), 0)
}

// refuseWrite says why a key that would write to the agent did nothing.
func (r *reviewScreen) refuseWrite(s panelShared) {
	reason, _ := r.isReadOnly(s)
	r.notice = reason
}

// refuseHistory says why a write did not happen on a round the agent has moved past, and names
// the key back to the work a comment could still ask it to change.
func (r *reviewScreen) refuseHistory(h host, s panelShared, said string) {
	summary := said + " is finished"
	detail := "This draws code the agent has already rewritten, so the line under the cursor is " +
		"not in the work it has now and a comment on it could not be acted on. Press " +
		keyLabel(s.keys.Review.BaseForward) + " until the whole change is on screen, then comment there."

	h.reportBrief(summary, detail)
	r.notice = summary + ", so nothing can be written on it"
}

// refuseViewed says why the fold key did nothing on a chapter, and names the key back to the
// change a mark is about.
func (r *reviewScreen) refuseViewed(h host, s panelShared) {
	summary := "a file is read in the whole change, not in one chapter"
	detail := "This draws one chapter of the change, so the file under the cursor is a different " +
		"diff here than it is in the whole of it and a mark taken on it would say you had read " +
		"something else. Press " + keyLabel(s.keys.Review.BaseForward) + " until the whole change " +
		"is on screen, then mark it there."

	h.reportBrief(summary, detail)
	r.notice = summary
}

// updateFilter is the search prompt taking keys.
func (r *reviewScreen) updateFilter(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	if key.String() == "ctrl+c" {
		return h.hide()
	}

	if !r.diff.filter.takeKey(key) {
		return nil
	}

	r.search(s)

	return nil
}

// search runs the pattern over the diff and moves to the first thing it finds.
func (r *reviewScreen) search(s panelShared) {
	if r.diff.view == nil {
		return
	}

	r.diff.search()
	r.scrollToCursor(s)
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

// jumpMatch steps to the next or previous line the search found, wrapping at the ends.
func (r *reviewScreen) jumpMatch(s panelShared, direction int) tea.Cmd {
	next, ok := r.diff.view.JumpMatch(r.diff.cursor, direction)
	if !ok {
		r.notice = "nothing to search for yet"
		if r.diff.filter.isStanding() {
			r.notice = "no matches"
		}

		return nil
	}

	return r.moveCursor(s, next)
}

// jumpOpenThread moves to the next comment nobody has settled, stopping at the ends.
func (r *reviewScreen) jumpOpenThread(s panelShared, direction int) tea.Cmd {
	next, ok := r.diff.view.JumpOpenThread(r.diff.cursor, direction)
	if !ok || next == r.diff.cursor {
		r.notice = r.diff.noOpenThreadSaid()

		return nil
	}

	return r.moveCursor(s, next)
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

// isOnChange reports whether the cursor stands on the row for the change as a whole.
func (d reviewDiff) isOnChange() bool {
	if d.view == nil {
		return false
	}

	anchor, ok := d.view.GetAnchor(d.cursor)

	return ok && anchor.FilePath == ""
}

// toggleExpanded draws the whole of the file the cursor is in, or gives back the changed lines
// and the few around them.
func (r *reviewScreen) toggleExpanded(s panelShared) {
	file, ok := r.diff.view.GetDrawnFileAt(r.diff.cursor)
	if !ok {
		return
	}

	path := file.Path()
	expanding := !r.diff.view.IsExpanded(path)

	if expanding && r.diff.view.IsViewed(path) {
		r.notice = "that file is folded away · " +
			keyLabel(s.keys.Review.ToggleViewed) + " to open it"

		return
	}

	// Where the cursor is standing is taken before the rows are built again.
	place, located := r.diff.view.GetPlace(r.diff.cursor)

	r.diff.view.SetFileExpanded(path, expanding)
	if r.diff.view.IsExpanded(path) != expanding {
		r.notice = "there is no more of that file to show"

		return
	}

	r.threads.blur()
	r.diff.keepCursor(place, located)
	r.scrollToCursor(s)

	r.notice = "showing the changed lines"
	if expanding {
		r.notice = "showing the whole file"
	}
}

// jumpFile moves to the next or previous file and puts that file's header at the top of the
// pane.
func (r *reviewScreen) jumpFile(s panelShared, direction int) tea.Cmd {
	next := r.diff.view.JumpFile(r.diff.cursor, direction)
	if next == r.diff.cursor {
		return nil
	}

	r.diff.cursor = next
	r.threads.blur()
	r.diff.top = max(min(r.diff.view.GetFileTopRow(next), r.diff.view.Len()-1), 0)
	r.diff.pinTop = true
	r.diff.pinFloor = r.diff.top
	r.scrollToCursor(s)

	return r.markThreadReadCmd(s)
}

func (r *reviewScreen) moveCursor(s panelShared, next int) tea.Cmd {
	if next == r.diff.cursor {
		return nil
	}

	r.diff.cursor = next
	r.diff.pinTop = false

	// The keys and the window both belong to the conversation the cursor was standing on, so
	// neither means anything against the next one it lands on.
	r.threads.blur()
	r.scrollToCursor(s)

	return r.markThreadReadCmd(s)
}

func (r *reviewScreen) scroll(s panelShared, direction int) tea.Cmd {
	return r.step(s, direction, navPageStep)
}

// reviewWheelStep is how many rows one scroll key moves the diff.
const reviewWheelStep = 3

func (r *reviewScreen) wheelPressed(h host, s panelShared, pressed string) tea.Cmd {
	if isBound(pressed, s.keys.Column.ScrollUp) {
		return r.step(s, -1, reviewWheelStep)
	}

	return r.step(s, 1, reviewWheelStep)
}

func (r *reviewScreen) scrollList(_ host, s panelShared, rows int) tea.Cmd {
	if _, ok := r.diff.threadAtCursor(); !ok {
		return nil
	}

	r.scrollThread(s, -rows*reviewWheelStep)

	return r.markThreadReadCmd(s)
}

func (r *reviewScreen) scrollColumn(_ host, s panelShared, rows int) tea.Cmd {
	if r.isWriting() {
		return nil
	}

	if rows < 0 {
		return r.step(s, -1, -rows)
	}

	return r.step(s, 1, rows)
}

func (r *reviewScreen) step(s panelShared, direction, rows int) tea.Cmd {
	cursor := r.diff.cursor
	for range rows {
		cursor = r.diff.view.Move(cursor, direction)
	}

	return r.moveCursor(s, cursor)
}

// panStep is a third of the pane, or the smallest jump until the pane is measured.
func panStep(s panelShared) int {
	split, ok := s.draw.screenColumnSplit()
	if !ok {
		return reviewPanStep
	}

	return max(split.column/3, reviewPanStep)
}

// panBy moves the diff sideways, stopping at its left edge.
func (d *reviewDiff) panBy(delta int) {
	d.pan = max(0, d.pan+delta)
}

// scrollToCursor moves the window only when the cursor is about to leave it.
func (r *reviewScreen) scrollToCursor(s panelShared) {
	split, ok := s.draw.screenColumnSplit()
	if !ok {
		return
	}

	r.diff.scrollToCursor(split.rows)
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
