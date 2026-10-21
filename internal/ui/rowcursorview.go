package ui

import (
	"komrad/internal/ui/paint"
	"komrad/internal/view"
)

func caretGlyph(caret view.Caret) string {
	if caret != view.CaretRendered {
		return blankCursor
	}

	return renderedCaret()
}

func renderedCaret() string {
	return paint.Panel.Cursor().Render(rowCursorGlyph)
}
