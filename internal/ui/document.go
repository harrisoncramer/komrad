package ui

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"

	"charm.land/glamour/v2/ansi"
)

// docWindow is where in a document the reader is: the rows it was rendered to, the first of them
// on screen, and whether it counts off what is past either end.
type docWindow struct {
	rows []string
	top  int
	// markers is whether the window counts the rows above and below it, which a document read from
	// the top wants and a transcript opened on a match does not.
	markers bool
}

// shape is the window as the reader package measures it.
func (d docWindow) shape() reader.Window {
	return reader.Window{Len: len(d.rows), Top: d.top, Markers: d.markers}
}

// clamp keeps the window inside the document.
func (d docWindow) clamp(top, rows int) int {
	return d.shape().Clamp(top, rows)
}

// view is the screenful of the document from the row the reader has scrolled to, padded out
// rather than left short so a document smaller than the window keeps its half instead of the
// list sliding under.
func (d docWindow) view(rows int) string {
	top, end := d.shape().Span(rows)

	out := make([]string, 0, rows)
	if d.markers && top > 0 {
		out = append(out, paint.MoreAbove(top, dimStyle))
	}

	out = append(out, d.rows[top:end]...)
	if rest := len(d.rows) - end; d.markers && rest > 0 && len(out) < rows {
		out = append(out, paint.MoreBelow(rest, dimStyle))
	}

	for len(out) < rows {
		out = append(out, "")
	}

	return strings.Join(out, "\n")
}

// columnStyle is the markdown the block beside the list is drawn in.
func columnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
