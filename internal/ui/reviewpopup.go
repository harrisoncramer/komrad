package ui

import (
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
)

// popupStage is the area the review's popup is placed over and how it is placed there.
type popupStage struct {
	area   string
	anchor popupAnchor
}

// stageColumn is the band and sidebar drawn as one column, with the popup beside the reader's place
// in it.
func (r reviewScreen) stageColumn(s panelShared) popupStage {
	band := r.bandView(s, s.pane)
	layout := r.layoutSidebar(s, band)
	area := strings.TrimRight(band+r.drawSidebar(layout), "\n")

	return popupStage{
		area: area,
		anchor: popupAnchor{
			cursor: paint.CountLines(band+layout.bar+layout.files+r.placeView()) - 1,
			edge:   s.pane,
			right:  s.pane,
		},
	}
}

// stageDiff is the band and the diff under it, with the popup kept clear of the code and free to
// run over the sidebar when it stands to the right.
func (r reviewScreen) stageDiff(s panelShared, split columnSplit) popupStage {
	band := r.bandView(s, split.column)

	right := split.column
	if s.draw.isSidebarOnRight() {
		right += dividerCols + split.sidebar
	}

	return popupStage{
		area: band + r.diffView(s, split),
		anchor: popupAnchor{
			cursor:     strings.Count(band, "\n") + r.diff.GetCursorRow(split.rows),
			edge:       split.column,
			right:      right,
			code:       diffview.GutterWidth,
			keepsClear: true,
		},
	}
}

// measurePopupInner is the room inside the popup where it stands over the review now.
func (r reviewScreen) measurePopupInner(s panelShared) int {
	placed := r.stageColumn(s)
	if split, ok := s.draw.screenColumnSplit(); ok {
		placed = r.stageDiff(s, split)
	}

	return placePopup(placed.area, placed.anchor).inner()
}

// popupBody is what the popup holds where the cursor stands: the editor while the reader writes,
// the conversation on the line otherwise, and nothing on a line with no thread.
func (r reviewScreen) popupBody(s panelShared, inner int) string {
	switch {
	case r.loading || r.diff.View() == nil || r.diff.View().Len() == 0:
		return ""
	case r.threads.composing:
		return r.threads.editor.view()
	case r.threads.releasing:
		return r.releaseView(inner)
	}

	id, body, ok := r.conversationAtCursor(s, inner)
	if !ok {
		return ""
	}

	return r.threads.scroll.cut(id, body)
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
