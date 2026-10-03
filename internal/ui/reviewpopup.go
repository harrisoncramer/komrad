package ui

import (
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
)

// popupAnchor is the columns the popup takes beside the diff's code: the sidebar, the divider and
// the gutter when the sidebar stands to the left, the divider and the sidebar when it stands to
// the right, and the whole pane without a split.
func (r reviewScreen) popupAnchor(m *model) popupAnchor {
	split, ok := m.screenColumnSplit()
	switch {
	case !ok:
		return popupAnchor{right: m.paneWidth()}
	case m.isSidebarOnRight():
		return popupAnchor{left: split.column, right: split.column + dividerCols + split.sidebar}
	default:
		return popupAnchor{right: split.sidebar + dividerCols + diffview.GutterWidth, codeOnRight: true}
	}
}

// measurePopupInner is the room inside the popup over the review.
func (r reviewScreen) measurePopupInner(m *model) int {
	return r.popupAnchor(m).measureWidth() - popupChrome
}

// popupBody is what the popup holds where the cursor stands and how far it is scrolled back from
// the end: the editor while the reader writes, the conversation on the line otherwise, and nothing
// on a line with no thread.
func (r reviewScreen) popupBody(m *model, inner int) (string, int) {
	switch {
	case r.loading || r.diff.View() == nil || r.diff.View().Len() == 0:
		return "", 0
	case r.threads.composing:
		return r.threads.editor.view(), 0
	case r.threads.releasing:
		return r.releaseView(inner), 0
	}

	id, body, ok := r.conversationAtCursor(m, inner)
	if !ok {
		return "", 0
	}

	return body, r.threads.scroll.back(id)
}

// conversationAtCursor is the conversation on the cursor's row, wrapped to inner.
func (r reviewScreen) conversationAtCursor(m *model, inner int) (string, string, bool) {
	if thread, ok := r.diff.threadAtCursor(); ok {
		return thread.GetId(), r.threadView(m, thread, inner), true
	}

	return "", "", false
}

// placeThread is where a popup holding a body stands beside the cursor's row of the review.
func (r reviewScreen) placeThread(m *model, body string) popupSlot {
	return placePopup(r.popupAnchor(m), r.measurePopupStage(m), len(splitPopupRows(body))+2)
}

// measurePopupStage is the rows the popup stands over, the band and the diff beside the sidebar or
// the band and the sidebar without a split, less the bar at the top that carries the messages.
func (r reviewScreen) measurePopupStage(m *model) popupStage {
	split, ok := m.screenColumnSplit()
	if !ok {
		band := r.bandView(m)
		layout := r.layoutSidebar(m, band)

		cursor := paint.CountLines(band+layout.bar+layout.change+layout.pullRequest+layout.files+r.placeView()) - 1

		return popupStage{
			top:        paint.CountLines(band + layout.bar),
			cursor:     cursor,
			cursorLast: cursor,
			rows:       paint.CountLines(strings.TrimRight(band+r.drawSidebar(layout), "\n")),
		}
	}

	above := strings.Count(r.bandView(m), "\n")
	first, height := r.diff.GetCursorLines(r.diffWindow(m))

	return popupStage{
		top:        strings.Count(r.layoutSidebar(m).bar, "\n"),
		cursor:     above + first,
		cursorLast: above + first + height - 1,
		rows:       above + split.rows,
	}
}
