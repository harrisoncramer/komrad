package reader

import (
	"strings"

	"komrad/internal/ui/paint"
)

// View is the screenful of the document from the row the reader has scrolled to, padded out
// rather than left short so a document smaller than the window keeps its half instead of the
// list sliding under.
func (p Page) View(rows int) string {
	top, end := p.Shape().Span(rows)

	out := make([]string, 0, rows)
	if p.HasMarkers && top > 0 {
		out = append(out, paint.MoreAbove(top, paint.Panel.Style(paint.ToneMuted)))
	}

	out = append(out, p.Rows[top:end]...)
	if rest := len(p.Rows) - end; p.HasMarkers && rest > 0 && len(out) < rows {
		out = append(out, paint.MoreBelow(rest, paint.Panel.Style(paint.ToneMuted)))
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return strings.Join(out, "\n")
}
