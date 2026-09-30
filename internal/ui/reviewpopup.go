package ui

const reviewPopupLeft = 13

// measureReviewPopupInner is the room inside a popup over the review's diff.
func measureReviewPopupInner(s panelShared) int {
	column, left := s.pane, 0
	if split, ok := s.draw.screenColumnSplit(); ok {
		column, left = split.column, reviewPopupLeft
	}

	return measurePopupInner(column - left)
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
