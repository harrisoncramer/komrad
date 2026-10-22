package ui

import (
	"strings"

	"komrad/internal/ui/paint"
)

// view is the screenful of the document from the row the reader has scrolled to, padded out
// rather than left short so a document smaller than the window keeps its half instead of the
// list sliding under.
func (d docWindow) view(rows int) string {
	top, end := d.shape().Span(rows)

	out := make([]string, 0, rows)
	if d.markers && top > 0 {
		out = append(out, paint.MoreAbove(top, paint.Panel.Style(paint.ToneMuted)))
	}

	out = append(out, d.rows[top:end]...)
	if rest := len(d.rows) - end; d.markers && rest > 0 && len(out) < rows {
		out = append(out, paint.MoreBelow(rest, paint.Panel.Style(paint.ToneMuted)))
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return strings.Join(out, "\n")
}
