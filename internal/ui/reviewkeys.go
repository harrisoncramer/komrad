package ui

import (
	"fmt"
	"slices"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

func (r *reviewScreen) updateKey(m *model, key tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := r.takeKey(m, key); ok {
		m.dropTopPress()

		return cmd
	}

	return r.actions(m).dispatchTop(m, key.String())
}

// takeKey handles a key before the screen's actions see it, and false for one it leaves to them.
func (r *reviewScreen) takeKey(m *model, key tea.KeyPressMsg) (tea.Cmd, bool) {
	if r.threads.composing {
		return r.updateComposer(m, key), true
	}

	if r.threads.releasing {
		return r.updateRelease(m, key), true
	}

	if r.filesFilter.typing {
		return r.updateFilesFilter(m, key), true
	}

	if r.diff.filter.typing {
		return r.diffKeys().updateFilter(m, key), true
	}

	k := key.String()

	// The diff opens for an agent that is not there, and only the back key answers until it has
	// something to draw.
	if r.diff.View() == nil {
		if isBound(k, m.keys.Review.Back) {
			return r.back(m), true
		}

		return nil, true
	}

	if isBound(k, slices.Concat(m.keys.Column.ScrollDown, m.keys.Column.ScrollUp)) && r.scrollPopup(m, directionOf(k, m.keys.Column.ScrollUp)*diffWheelStep) {
		return nil, true
	}

	return nil, false
}

// refuseWrite says why a key that would write to the agent did nothing.
func (r *reviewScreen) refuseWrite(m *model) {
	m.note(messageWarning, archivedSaid)
}

// refuseHistory says why a write did not happen on a round the agent has moved past, and names
// the key back to the work a comment could still ask it to change.
func (r *reviewScreen) refuseHistory(m *model, said string) {
	summary := said + " is finished"
	back := keyLabel(m.keys.Review.BaseForward) + " until the whole change is on screen"
	if r.diff.spec.origin != nil {
		back = keyLabel(m.keys.Review.Jump) + " to go back to where you were, then " + back
	}

	detail := "This draws code the agent has already rewritten, so the line under the cursor is " +
		"not in the work it has now and a comment on it could not be acted on. Press " + back +
		", then comment there."

	m.reportBrief(summary, detail)
}

// refuseViewed says why the fold key did nothing on a chapter, and names the key back to the
// change a mark is about.
func (r *reviewScreen) refuseViewed(m *model) {
	summary := "a file is read in the whole change, not in one chapter"
	detail := "This draws one chapter of the change, so the file under the cursor is a different " +
		"diff here than it is in the whole of it and a mark taken on it would say you had read " +
		"something else. Press " + keyLabel(m.keys.Review.BaseForward) + " until the whole change " +
		"is on screen, then mark it there."

	m.reportBrief(summary, detail)
}

// diffKeys is the review's own diff under the keys every diff pane shares, with the cursor
// landing on a conversation it has to answer for.
func (r *reviewScreen) diffKeys() diffKeys {
	return diffKeys{
		pane:   &r.diff.Pane,
		filter: &r.diff.filter,
		window: r.diffWindow,
		moved: func(m *model) tea.Cmd {
			r.threads.scroll.reset()

			return m.waitToStage()
		},
		refuseExpand: r.refuseExpand,
	}
}

// refuseExpand says why the file under the cursor cannot be drawn whole.
func (r *reviewScreen) refuseExpand(m *model) string {
	file, ok := r.diff.View().GetDrawnFileAt(r.diff.Cursor())
	if !ok || r.diff.View().IsExpanded(file.Path()) || !r.diff.View().IsViewed(file.Path()) {
		return ""
	}

	return "that file is folded away" + separated + keyLabel(m.keys.Review.ToggleViewed) + " to open it"
}

// diffWindow is the window the diff is drawn in, zero-sized without a split.
func (r *reviewScreen) diffWindow(m *model) diffview.Window {
	split, ok := m.screenColumnSplit()
	if !ok {
		return diffview.Window{Wrap: m.diffWrap}
	}

	return diffview.Window{
		Width:  split.column,
		Height: split.rows,
		Wrap:   m.diffWrap,
	}
}

// jumpOpenThread moves to the next comment nobody has settled, stopping at the ends.
func (r *reviewScreen) jumpOpenThread(m *model, direction int) tea.Cmd {
	next, ok := r.diff.View().JumpOpenThread(r.diff.Cursor(), direction)
	if !ok || next == r.diff.Cursor() {
		m.note(messageWarning, r.diff.noOpenThreadSaid())

		return nil
	}

	return r.moveCursor(m, next)
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

func (r *reviewScreen) moveCursor(m *model, next int) tea.Cmd {
	return r.diffKeys().moveTo(m, next)
}

func (r *reviewScreen) doesScrollColumn(*model) bool {
	return true
}

func (r *reviewScreen) scrollColumn(m *model, rows int) tea.Cmd {
	if r.isWriting() || r.diff.View() == nil {
		return nil
	}

	return r.diffKeys().moveBy(m, rows)
}

// scrollList presses the scroll key once for every row, past the popup.
func (r *reviewScreen) scrollList(m *model, rows int) tea.Cmd {
	if r.isWriting() || r.diff.View() == nil {
		return nil
	}

	pressed := m.keys.Column.ScrollDown
	if rows < 0 {
		pressed = m.keys.Column.ScrollUp
	}

	if len(pressed) == 0 {
		return nil
	}

	var cmds []tea.Cmd
	for range max(rows, -rows) {
		cmds = append(cmds, r.diffActions().dispatch(m, pressed[0]))
	}

	return tea.Batch(cmds...)
}

// scrollPopupAt moves the conversation in the popup when the cell at x and y is inside it.
func (r *reviewScreen) scrollPopupAt(m *model, x, y, rows int) bool {
	body, _ := r.popupBody(m, r.measurePopupInner(m))
	if body == "" || !r.placeThread(m, body).contains(x, y) {
		return false
	}

	if !r.isWriting() {
		r.scrollPopup(m, rows*diffWheelStep)
	}

	return true
}

// scrollToCursor moves the window only when the cursor is about to leave it.
func (r *reviewScreen) scrollToCursor(m *model) {
	r.diff.ScrollToCursor(r.diffWindow(m))
}

// scrollPopup moves the conversation in the popup down by rows, up when rows is negative, and
// reports whether there was a conversation there to move.
func (r *reviewScreen) scrollPopup(m *model, rows int) bool {
	id, body, ok := r.conversationAtCursor(m, r.measurePopupInner(m))
	if !ok {
		return false
	}

	r.threads.scroll.move(id, body, rows, r.placeThread(m, body).room())

	return true
}

// scrollSidebarAt walks the file tree or the pull request description when row y is inside it,
// without focusing it.
func (r *reviewScreen) scrollSidebarAt(m *model, y, rows int) (tea.Cmd, bool) {
	section, ok := r.sidebarNavAt(m, y)
	if !ok {
		return nil, false
	}

	if r.isWriting() {
		return nil, true
	}

	return section.moveBy(m, rows), true
}

// sidebarNavAt is the sidebar section drawn on row y, and false for a row the wheel leaves alone.
func (r *reviewScreen) sidebarNavAt(m *model, y int) (navigable, bool) {
	if _, ok := m.screenColumnSplit(); !ok {
		return nil, false
	}

	layout := r.layoutSidebar(m)
	pullRequestTop := paint.CountLines(layout.bar + layout.change)
	filesTop := pullRequestTop + paint.CountLines(layout.pullRequest)

	switch {
	case y >= pullRequestTop && y < filesTop && r.isPullRequestDrawn(m):
		return pullRequestNav{r}, true
	case y >= filesTop && y < filesTop+paint.CountLines(layout.files) && r.isBlockDrawn(m):
		return filesNav{r}, true
	}

	return nil, false
}
