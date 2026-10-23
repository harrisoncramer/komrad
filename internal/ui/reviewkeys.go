package ui

import (
	"fmt"
	"image"
	"slices"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

func (r *reviewScreen) updateKey(in screen.Inputs, key tea.KeyPressMsg) screen.Request {
	if request, ok := r.takeKey(in, key); ok {
		r.deps.top.Drop()

		return request
	}

	return r.actions(in).DispatchTop(r.deps.top, r, r.deps.keys, key.String())
}

// takeKey handles a key before the screen's actions see it, and false for one it leaves to them.
func (r *reviewScreen) takeKey(in screen.Inputs, key tea.KeyPressMsg) (screen.Request, bool) {
	if r.threads.composing {
		return r.updateComposer(in, key), true
	}

	if r.filesFilter.Typing {
		return r.updateFilesFilter(in, key), true
	}

	if r.diff.filter.Typing {
		return r.diffKeys().UpdateFilter(in, key), true
	}

	k := key.String()

	// The diff opens for an agent that is not there, and only the back key answers until it has
	// something to render.
	if r.diff.View() == nil {
		if keys.IsBound(k, fixedKeys().Back) {
			return r.back(in), true
		}

		return nil, true
	}

	return nil, false
}

// PressPopup moves the conversation in the popup on a scroll key nothing ahead of the popup takes.
func (r *reviewScreen) PressPopup(in screen.Inputs, chrome screen.Chrome, pressed string) (screen.Request, bool) {
	if r.threads.composing || r.filesFilter.Typing || r.diff.filter.Typing || r.diff.View() == nil {
		return nil, false
	}

	if !keys.IsBound(pressed, slices.Concat(r.deps.keys.Column.ScrollDown, r.deps.keys.Column.ScrollUp)) || !r.scrollPopup(in, chrome, keys.DirectionOf(pressed, r.deps.keys.Column.ScrollUp)*keys.DiffWheelStep) {
		return nil, false
	}

	r.deps.top.Drop()

	return r.followWorld(in), true
}

// diffKeys is the review's own diff under the keys every diff pane shares, with the cursor
// landing on a conversation it has to answer for.
func (r *reviewScreen) diffKeys() keys.Diff {
	return keys.Diff{
		Keys:     r.deps.keys,
		Register: r.deps.register,
		Pane:     &r.diff.Pane,
		Filter:   &r.diff.filter,
		Window:   r.diffWindow,
		Moved: func() screen.Request {
			r.threads.scroll.reset()

			return screen.Settle{}
		},
		RefuseExpand: r.refuseExpand,
	}
}

// refuseExpand says why the file under the cursor cannot render whole.
func (r *reviewScreen) refuseExpand() string {
	file, ok := r.diff.View().GetRenderedFileAt(r.diff.Cursor())
	if !ok || r.diff.View().IsExpanded(file.Path()) || !r.diff.View().IsViewed(file.Path()) {
		return ""
	}

	return "that file is folded away" + separated + menuChordLabel(r.deps.keys, r.deps.keys.Review.ToggleViewed) + " to open it"
}

// diffWindow is the window that renders the diff, zero-sized without a split.
func (r *reviewScreen) diffWindow(in screen.Inputs) diffview.Window {
	split := in.Room.Split
	if !split.On {
		return diffview.Window{Wrap: in.Room.Wrap}
	}

	return diffview.Window{
		Width:  split.Column,
		Height: split.Rows,
		Wrap:   in.Room.Wrap,
	}
}

// jumpOpenThread moves to the next comment nobody has settled, stopping at the ends.
func (r *reviewScreen) jumpOpenThread(in screen.Inputs, direction int) screen.Request {
	next, ok := r.diff.View().JumpOpenThread(r.diff.Cursor(), direction)
	if !ok || next == r.diff.Cursor() {
		return screen.NewNote(screen.Warning, r.diff.noOpenThreadSaid())
	}

	return r.moveCursor(in, next)
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

func (r *reviewScreen) moveCursor(in screen.Inputs, next int) screen.Request {
	return r.diffKeys().MoveTo(in, next)
}

func (r *reviewScreen) DoesScrollColumn(screen.Inputs) bool {
	return true
}

func (r *reviewScreen) ScrollColumn(in screen.Inputs, rows int) screen.Request {
	if r.isWriting() || r.diff.View() == nil {
		return nil
	}

	return r.diffKeys().MoveBy(in, rows)
}

// ScrollList presses the scroll key once for every row, past the popup.
func (r *reviewScreen) ScrollList(in screen.Inputs, rows int) screen.Request {
	if r.isWriting() || r.diff.View() == nil {
		return nil
	}

	pressed := r.deps.keys.Column.ScrollDown
	if rows < 0 {
		pressed = r.deps.keys.Column.ScrollUp
	}

	if len(pressed) == 0 {
		return nil
	}

	var requests screen.Batch
	for range max(rows, -rows) {
		requests = append(requests, r.diffActions(in).Dispatch(r, r.deps.keys, pressed[0]))
	}

	return requests
}

// ScrollPopupAt moves the conversation in the popup when the popup holds the cell.
func (r *reviewScreen) ScrollPopupAt(in screen.Inputs, chrome screen.Chrome, at image.Point, rows int) bool {
	body, _ := r.popupBody(in, r.measurePopupInner(in))
	if body == "" || !r.placeThread(in, chrome, body).contains(at.X, at.Y) {
		return false
	}

	if !r.isWriting() {
		r.scrollPopup(in, chrome, rows*keys.DiffWheelStep)
	}

	return true
}

// scrollToCursor moves the window only when the cursor is about to leave it.
func (r *reviewScreen) scrollToCursor(in screen.Inputs) {
	r.diff.ScrollToCursor(r.diffWindow(in))
}

// scrollPopup moves the conversation in the popup down by rows, up when rows is negative, and
// reports whether there was a conversation there to move.
func (r *reviewScreen) scrollPopup(in screen.Inputs, chrome screen.Chrome, rows int) bool {
	id, body, ok := r.conversationAtCursor(in, r.measurePopupInner(in))
	if !ok {
		return false
	}

	r.threads.scroll.move(id, body, rows, r.placeThread(in, chrome, body).room())

	return true
}

// ScrollSidebarAt walks the file tree or the pull request description when row y is inside it,
// without focusing it.
func (r *reviewScreen) ScrollSidebarAt(in screen.Inputs, chrome screen.Chrome, y, rows int) (screen.Request, bool) {
	section, ok := r.sidebarNavAt(in, chrome, y)
	if !ok {
		return nil, false
	}

	if r.isWriting() {
		return nil, true
	}

	return section.MoveBy(in, rows), true
}

// sidebarNavAt is the sidebar section rendered on row y, and false for a row the wheel leaves alone.
func (r *reviewScreen) sidebarNavAt(in screen.Inputs, chrome screen.Chrome, y int) (keys.Navigable, bool) {
	if !in.Room.Split.On {
		return nil, false
	}

	layout := r.layoutSidebar(in, chrome)
	filesTop := paint.CountLines(layout.bar + layout.change + layout.side)
	if y >= filesTop && y < filesTop+paint.CountLines(layout.files) && r.IsBlockRendered(in) {
		return filesNav{r}, true
	}

	return nil, false
}
