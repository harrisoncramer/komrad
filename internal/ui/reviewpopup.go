package ui

import (
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
)

// popupAnchor is the columns the popup takes beside the diff's code: the sidebar, the divider and
// the gutter when the sidebar stands to the left, the divider and the sidebar when it stands to
// the right, and the whole pane without a split.
func (r reviewScreen) popupAnchor(s panelShared) popupAnchor {
	split, ok := s.draw.screenColumnSplit()
	switch {
	case !ok:
		return popupAnchor{right: s.pane}
	case s.draw.isSidebarOnRight():
		return popupAnchor{left: split.column, right: split.column + dividerCols + split.sidebar}
	default:
		return popupAnchor{right: split.sidebar + dividerCols + diffview.GutterWidth, codeOnRight: true}
	}
}

// measurePopupInner is the room inside the popup over the review.
func (r reviewScreen) measurePopupInner(s panelShared) int {
	return r.popupAnchor(s).measureWidth() - popupChrome
}

// popupBody is what the popup holds where the cursor stands and how far it is scrolled back from
// the end: the editor while the reader writes, the conversation on the line otherwise, and nothing
// on a line with no thread.
func (r reviewScreen) popupBody(s panelShared, inner int) (string, int) {
	switch {
	case r.loading || r.diff.View() == nil || r.diff.View().Len() == 0:
		return "", 0
	case r.threads.composing:
		return r.threads.editor.view(), 0
	case r.threads.releasing:
		return r.releaseView(inner), 0
	}

	id, body, ok := r.conversationAtCursor(s, inner)
	if !ok {
		return "", 0
	}

	return body, r.threads.scroll.back(id)
}

// conversationAtCursor is the conversation on the cursor's row, a thread of the reader's ahead of
// the forge's, wrapped to inner.
func (r reviewScreen) conversationAtCursor(s panelShared, inner int) (string, string, bool) {
	if thread, ok := r.diff.threadAtCursor(); ok {
		return thread.GetId(), r.threadView(s, thread, inner), true
	}

	if offer, ok := r.offerAtCursor(s); ok {
		return offer.GetId(), r.offerView(offer, inner), true
	}

	return "", "", false
}

// placeThread is where a popup holding a body stands beside the cursor's row of the review.
func (r reviewScreen) placeThread(s panelShared, body string) popupSlot {
	return placePopup(r.popupAnchor(s), r.measurePopupStage(s), len(splitPopupRows(body))+2)
}

// measurePopupStage is the rows the popup stands over, the band and the diff beside the sidebar or
// the band and the sidebar without a split, less the bar at the top that carries the messages.
func (r reviewScreen) measurePopupStage(s panelShared) popupStage {
	split, ok := s.draw.screenColumnSplit()
	if !ok {
		band := r.bandView(s, s.pane)
		layout := r.layoutSidebar(s, band)

		return popupStage{
			top:    paint.CountLines(band + layout.bar),
			cursor: paint.CountLines(band+layout.bar+layout.files+r.placeView()) - 1,
			rows:   paint.CountLines(strings.TrimRight(band+r.drawSidebar(layout), "\n")),
		}
	}

	above := strings.Count(r.bandView(s, split.column), "\n")

	return popupStage{
		top:    strings.Count(r.layoutSidebar(s).bar, "\n"),
		cursor: above + r.diff.GetCursorRow(split.rows),
		rows:   above + split.rows,
	}
}
