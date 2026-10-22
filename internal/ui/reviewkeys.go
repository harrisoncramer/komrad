package ui

import (
	"fmt"
	"slices"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

func (r *reviewScreen) updateKey(m *model, key tea.KeyPressMsg) screen.Request {
	if request, ok := r.takeKey(m, key); ok {
		m.dropTopPress()

		return request
	}

	return m.dispatchTop(r.actions(m), key.String())
}

// takeKey handles a key before the screen's actions see it, and false for one it leaves to them.
func (r *reviewScreen) takeKey(m *model, key tea.KeyPressMsg) (screen.Request, bool) {
	if r.threads.composing {
		return r.updateComposer(m, key), true
	}

	if r.filesFilter.typing {
		return r.updateFilesFilter(m, key), true
	}

	if r.diff.filter.typing {
		return r.diffKeys().updateFilter(m, key), true
	}

	k := key.String()

	// The diff opens for an agent that is not there, and only the back key answers until it has
	// something to render.
	if r.diff.View() == nil {
		if keys.IsBound(k, fixedKeys().Back) {
			return r.back(m), true
		}

		return nil, true
	}

	if keys.IsBound(k, slices.Concat(m.keys.Column.ScrollDown, m.keys.Column.ScrollUp)) && r.scrollPopup(m, keys.DirectionOf(k, m.keys.Column.ScrollUp)*diffWheelStep) {
		return nil, true
	}

	return nil, false
}

// diffKeys is the review's own diff under the keys every diff pane shares, with the cursor
// landing on a conversation it has to answer for.
func (r *reviewScreen) diffKeys() diffKeys {
	return diffKeys{
		pane:   &r.diff.Pane,
		filter: &r.diff.filter,
		window: r.diffWindow,
		moved: func(m *model) screen.Request {
			r.threads.scroll.reset()

			return screen.Settle{}
		},
		refuseExpand: r.refuseExpand,
	}
}

// refuseExpand says why the file under the cursor cannot render whole.
func (r *reviewScreen) refuseExpand(m *model) string {
	file, ok := r.diff.View().GetRenderedFileAt(r.diff.Cursor())
	if !ok || r.diff.View().IsExpanded(file.Path()) || !r.diff.View().IsViewed(file.Path()) {
		return ""
	}

	return "that file is folded away" + separated + m.menuChordLabel(m.keys.Review.ToggleViewed) + " to open it"
}

// diffWindow is the window that renders the diff, zero-sized without a split.
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
func (r *reviewScreen) jumpOpenThread(m *model, direction int) screen.Request {
	next, ok := r.diff.View().JumpOpenThread(r.diff.Cursor(), direction)
	if !ok || next == r.diff.Cursor() {
		return screen.NewNote(screen.Warning, r.diff.noOpenThreadSaid())
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

func (d reviewDiff) threadByID(id string) (*agentv1.ReviewThread, bool) {
	if d.View() == nil || id == "" {
		return nil, false
	}

	row, ok := d.View().GetThreadRow(id)
	if !ok {
		return nil, false
	}

	return d.View().GetThreadAt(row)
}

func (r *reviewScreen) moveCursor(m *model, next int) screen.Request {
	return r.diffKeys().moveTo(m, next)
}

func (r *reviewScreen) doesScrollColumn(*model) bool {
	return true
}

func (r *reviewScreen) scrollColumn(m *model, rows int) screen.Request {
	if r.isWriting() || r.diff.View() == nil {
		return nil
	}

	return r.diffKeys().moveBy(m, rows)
}

// scrollList presses the scroll key once for every row, past the popup.
func (r *reviewScreen) scrollList(m *model, rows int) screen.Request {
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

	var requests screen.Batch
	for range max(rows, -rows) {
		requests = append(requests, r.diffActions().Dispatch(m, m.keys, pressed[0]))
	}

	return requests
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
func (r *reviewScreen) scrollSidebarAt(m *model, y, rows int) (screen.Request, bool) {
	section, ok := r.sidebarNavAt(m, y)
	if !ok {
		return nil, false
	}

	if r.isWriting() {
		return nil, true
	}

	return section.moveBy(m, rows), true
}

// sidebarNavAt is the sidebar section rendered on row y, and false for a row the wheel leaves alone.
func (r *reviewScreen) sidebarNavAt(m *model, y int) (navigable, bool) {
	if _, ok := m.screenColumnSplit(); !ok {
		return nil, false
	}

	layout := r.layoutSidebar(m)
	filesTop := paint.CountLines(layout.bar + layout.change + layout.side)
	if y >= filesTop && y < filesTop+paint.CountLines(layout.files) && r.isBlockRendered(m) {
		return filesNav{r}, true
	}

	return nil, false
}
