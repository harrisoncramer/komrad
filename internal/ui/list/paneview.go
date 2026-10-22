package list

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
)

// Blank is the room a row leaves where the arrow would stand.
const Blank = "  "

// Arrow is the arrow a list renders on the row the cursor is standing on.
const Arrow = glyph.Focus + " "

// ArrowOf is the arrow as a row with the caret renders it, or the blank it leaves.
func ArrowOf(caret view.Caret) string {
	if caret != view.CaretRendered {
		return Blank
	}

	return RenderArrow()
}

// RenderArrow is the arrow in the panel's cursor colour.
func RenderArrow() string {
	return paint.Panel.Cursor().Render(Arrow)
}

// Prepare resolves a snapshot to the list rendered from it, in the glyphs the panel was
// configured with.
func Prepare(glyphs glyph.Set, snapshot view.Snapshot) view.List {
	snapshot.Glyphs = glyphs

	return view.Prepare(snapshot)
}
