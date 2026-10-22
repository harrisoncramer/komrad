package ui

import (
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
)

// popupAnchor is the columns the popup takes beside the diff's code, which are the sidebar, the
// divider and the gutter with a split and the whole pane without one.
func (r reviewScreen) popupAnchor(in screen.Inputs) popupAnchor {
	split := in.Room.Split
	if !split.On {
		return popupAnchor{right: in.Room.Width}
	}

	return popupAnchor{right: split.Sidebar + dividerCols + diffview.GutterWidth, codeOnRight: true}
}

// measurePopupInner is the room inside the popup over the review.
func (r reviewScreen) measurePopupInner(in screen.Inputs) int {
	return r.popupAnchor(in).measureWidth() - popupChrome
}

// popupBody is what the popup holds where the cursor stands and how far it is scrolled back from
// the end: the editor while the reader writes, under the conversation it replies in, the
// conversation on the line otherwise, and nothing on a line with no thread.
func (r reviewScreen) popupBody(m *model, in screen.Inputs, inner int) (string, int) {
	switch {
	case r.loading || r.diff.View() == nil || r.diff.View().Len() == 0:
		return "", 0
	case r.threads.composing:
		thread, ok := r.diff.threadByID(r.threads.editing)
		if !ok {
			return r.threads.editor.view(), 0
		}

		return withSection(r.threadView(m, thread, inner), r.threads.editor.view(), inner), 0
	}

	id, body, ok := r.conversationAtCursor(m, in, inner)
	if !ok {
		return "", 0
	}

	return body, r.threads.scroll.back(id)
}

// conversationAtCursor is the conversation on the cursor's row, wrapped to inner, and none while the
// sidebar has the keys.
func (r reviewScreen) conversationAtCursor(m *model, in screen.Inputs, inner int) (string, string, bool) {
	if r.isBlockFocused(m, in) {
		return "", "", false
	}

	if thread, ok := r.diff.threadAtCursor(); ok {
		return thread.GetId(), r.threadView(m, thread, inner), true
	}

	return "", "", false
}

// placeThread is where a popup holding a body stands beside the cursor's row of the review.
func (r reviewScreen) placeThread(m *model, in screen.Inputs, body string) popupSlot {
	return placePopup(r.popupAnchor(in), r.measurePopupStage(m, in), len(splitPopupRows(body))+2)
}

// isPopupForm reports whether the form stands in the review's popup rather than in a column of its own.
func (m *model) isPopupForm() bool {
	return m.form.isOpening() && m.screen == screen.Review && !m.review.hold.atEditor && !m.isMessagesFocused()
}

// measurePopupRoom is the rows inside the tallest popup the review has room for beside the cursor.
func (r reviewScreen) measurePopupRoom(m *model, in screen.Inputs) int {
	return placePopup(r.popupAnchor(in), r.measurePopupStage(m, in), popupHeight).room()
}

// measurePopupStage is the rows the popup stands over, the band and the diff beside the sidebar or
// the band and the sidebar without a split, less the bar at the top that carries the messages.
func (r reviewScreen) measurePopupStage(m *model, in screen.Inputs) popupStage {
	split := in.Room.Split
	if !split.On {
		band := r.bandView(m, in)
		layout := r.layoutSidebar(m, in, band)

		cursor := paint.CountLines(band+layout.bar+layout.change+layout.block()+r.placeView()) - 1

		return popupStage{
			top:        paint.CountLines(band + layout.bar),
			cursor:     cursor,
			cursorLast: cursor,
			rows:       paint.CountLines(strings.TrimRight(band+r.renderSidebar(layout, layout.block()), "\n")),
		}
	}

	above := strings.Count(r.bandView(m, in), "\n")
	first, height := r.diff.GetCursorLines(r.diffWindow(in))

	return popupStage{
		top:        strings.Count(r.layoutSidebar(m, in).bar, "\n"),
		cursor:     above + first,
		cursorLast: above + first + height - 1,
		rows:       above + split.Rows,
	}
}
