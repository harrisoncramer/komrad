package ui

import (
	"fmt"

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

	if r.diff.filter.typing {
		return r.diffKeys().updateFilter(h, s, key)
	}

	k := key.String()

	// The diff opens for an agent that is not there, and only the back key answers until it has
	// something to draw.
	if r.diff.View() == nil {
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
		r.scrollThread(s, diffWheelStep)

		return true
	case isBound(k, s.keys.Column.ScrollDown):
		r.scrollThread(s, -diffWheelStep)

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

// diffKeys is the review's own diff under the keys every diff pane shares, with the cursor
// landing on a conversation it has to answer for.
func (r *reviewScreen) diffKeys() diffKeys {
	return diffKeys{
		pane:   &r.diff.Pane,
		filter: &r.diff.filter,
		notice: &r.notice,
		height: r.diffHeight,
		moved: func(s panelShared) tea.Cmd {
			// The keys and the window both belong to the conversation the cursor was standing on,
			// so neither means anything against the next one it lands on.
			r.threads.blur()

			return r.markThreadReadCmd(s)
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
func (r *reviewScreen) jumpOpenThread(s panelShared, direction int) tea.Cmd {
	next, ok := r.diff.View().JumpOpenThread(r.diff.Cursor(), direction)
	if !ok || next == r.diff.Cursor() {
		r.notice = r.diff.noOpenThreadSaid()

		return nil
	}

	return r.moveCursor(s, next)
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

func (r *reviewScreen) moveCursor(s panelShared, next int) tea.Cmd {
	return r.diffKeys().moveTo(s, next)
}

func (r *reviewScreen) scrollList(_ host, s panelShared, rows int) tea.Cmd {
	if _, ok := r.diff.threadAtCursor(); !ok {
		return nil
	}

	r.scrollThread(s, -rows*diffWheelStep)

	return r.markThreadReadCmd(s)
}

func (r *reviewScreen) doesScrollColumn(panelShared) bool {
	return true
}

func (r *reviewScreen) scrollColumn(_ host, s panelShared, rows int) tea.Cmd {
	if r.isWriting() {
		return nil
	}

	return r.diffKeys().scrollColumn(s, rows)
}

// scrollToCursor moves the window only when the cursor is about to leave it.
func (r *reviewScreen) scrollToCursor(s panelShared) {
	r.diff.ScrollToCursor(r.diffHeight(s))
}
