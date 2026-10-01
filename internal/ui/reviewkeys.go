package ui

import (
	"fmt"
	"slices"

	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

func (r *reviewScreen) updateKey(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	if r.threads.composing {
		return r.updateComposer(h, s, key)
	}

	if r.threads.releasing {
		return r.updateRelease(h, s, key)
	}

	if r.filesFilter.typing {
		return r.updateFilesFilter(h, s, key)
	}

	if r.diff.filter.typing {
		return r.diffKeys().updateFilter(h, s, key)
	}

	k := key.String()

	// The diff opens for an agent that is not there, and only the back key answers until it has
	// something to draw.
	if r.diff.View() == nil {
		if isBound(k, s.keys.Review.Back) {
			return r.back(h, s)
		}

		return nil
	}

	if isBound(k, slices.Concat(s.keys.Column.ScrollDown, s.keys.Column.ScrollUp)) && r.scrollPopup(s, directionOf(k, s.keys.Column.ScrollUp)*diffWheelStep) {
		return nil
	}

	return r.actions(s).dispatch(h, s, k)
}

// refuseWrite says why a key that would write to the agent did nothing.
func (r *reviewScreen) refuseWrite(h host, s panelShared) {
	reason, _ := r.isReadOnly(s)
	h.note(messageWarning, reason)
}

// refuseHistory says why a write did not happen on a round the agent has moved past, and names
// the key back to the work a comment could still ask it to change.
func (r *reviewScreen) refuseHistory(h host, s panelShared, said string) {
	summary := said + " is finished"
	back := keyLabel(s.keys.Review.BaseForward) + " until the whole change is on screen"
	if r.diff.origin != nil {
		back = keyLabel(s.keys.Review.Jump) + " to go back to where you were, then " + back
	}

	detail := "This draws code the agent has already rewritten, so the line under the cursor is " +
		"not in the work it has now and a comment on it could not be acted on. Press " + back +
		", then comment there."

	h.reportBrief(summary, detail)
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
}

// diffKeys is the review's own diff under the keys every diff pane shares, with the cursor
// landing on a conversation it has to answer for.
func (r *reviewScreen) diffKeys() diffKeys {
	return diffKeys{
		pane:   &r.diff.Pane,
		filter: &r.diff.filter,
		height: r.diffHeight,
		moved: func(h navHost, _ panelShared) tea.Cmd {
			r.threads.scroll.reset()

			return h.waitToStage()
		},
		refuseExpand: r.refuseExpand,
	}
}

// refuseExpand says why the file under the cursor cannot be drawn whole.
func (r *reviewScreen) refuseExpand(s panelShared) string {
	file, ok := r.diff.View().GetDrawnFileAt(r.diff.Cursor())
	if !ok || r.diff.View().IsExpanded(file.Path()) || !r.diff.View().IsViewed(file.Path()) {
		return ""
	}

	return "that file is folded away" + separated + keyLabel(s.keys.Review.ToggleViewed) + " to open it"
}

// diffHeight is the rows the diff is drawn in.
func (r *reviewScreen) diffHeight(s panelShared) int {
	split, ok := s.draw.screenColumnSplit()
	if !ok {
		return 0
	}

	return split.rows
}

// jumpOpenThread moves to the next comment nobody has settled, stopping at the ends.
func (r *reviewScreen) jumpOpenThread(h navHost, s panelShared, direction int) tea.Cmd {
	next, ok := r.diff.View().JumpOpenThread(r.diff.Cursor(), direction)
	if !ok || next == r.diff.Cursor() {
		h.note(messageWarning, r.diff.noOpenThreadSaid())

		return nil
	}

	return r.moveCursor(h, s, next)
}

// noOpenThreadSaid is why the key moved nowhere.
func (d reviewDiff) noOpenThreadSaid() string {
	if d.View().IsOpenThreadAt(d.Cursor()) && d.View().CountOpenThreads() == 1 {
		return "this is the only comment left open"
	}

	if d.View().CountOpenThreads() > 0 {
		return "the comments left open are in files you have folded away"
	}

	return "every comment here has been settled"
}

// threadStepDesc names the three keys that walk the conversations, by what is left to settle.
func (d reviewDiff) threadStepDesc() string {
	if d.View() == nil {
		return "thread, next open"
	}

	open := d.View().CountOpenThreads()
	if open == 0 {
		return "thread, next open"
	}

	return fmt.Sprintf("thread, %d open", open)
}

// threadAtCursor is the thread the cursor is standing on, and false while the diff is still
// loading.
func (d reviewDiff) threadAtCursor() (*agentv1.ReviewThread, bool) {
	if d.View() == nil {
		return nil, false
	}

	return d.View().GetThreadAt(d.Cursor())
}

// isOnChange reports whether the cursor stands on the row for the change as a whole.
func (d reviewDiff) isOnChange() bool {
	if d.View() == nil {
		return false
	}

	anchor, ok := d.View().GetAnchor(d.Cursor())

	return ok && anchor.FilePath == ""
}

func (r *reviewScreen) moveCursor(h navHost, s panelShared, next int) tea.Cmd {
	return r.diffKeys().moveTo(h, s, next)
}

func (r *reviewScreen) doesScrollColumn(panelShared) bool {
	return true
}

func (r *reviewScreen) scrollColumn(h host, s panelShared, rows int) tea.Cmd {
	if r.isWriting() || r.diff.View() == nil {
		return nil
	}

	return r.diffKeys().scrollColumn(h, s, rows)
}

// scrollList presses the scroll key once for every row, past the popup.
func (r *reviewScreen) scrollList(h host, s panelShared, rows int) tea.Cmd {
	if r.isWriting() || r.diff.View() == nil {
		return nil
	}

	pressed := s.keys.Column.ScrollDown
	if rows < 0 {
		pressed = s.keys.Column.ScrollUp
	}

	if len(pressed) == 0 {
		return nil
	}

	var cmds []tea.Cmd
	for range max(rows, -rows) {
		cmds = append(cmds, r.actions(s).dispatch(h, s, pressed[0]))
	}

	return tea.Batch(cmds...)
}

// scrollPopupAt moves the conversation in the popup when the cell at x and y is inside it.
func (r *reviewScreen) scrollPopupAt(s panelShared, x, y, rows int) bool {
	body, _ := r.popupBody(s, r.measurePopupInner(s))
	if body == "" || !r.placeThread(s, body).contains(x, y) {
		return false
	}

	if !r.isWriting() {
		r.scrollPopup(s, rows*diffWheelStep)
	}

	return true
}

// scrollToCursor moves the window only when the cursor is about to leave it.
func (r *reviewScreen) scrollToCursor(s panelShared) {
	r.diff.ScrollToCursor(r.diffHeight(s))
}

// scrollPopup moves the conversation in the popup down by rows, up when rows is negative, and
// reports whether there was a conversation there to move.
func (r *reviewScreen) scrollPopup(s panelShared, rows int) bool {
	id, body, ok := r.conversationAtCursor(s, r.measurePopupInner(s))
	if !ok {
		return false
	}

	r.threads.scroll.move(id, body, rows, r.placeThread(s, body).room())

	return true
}
