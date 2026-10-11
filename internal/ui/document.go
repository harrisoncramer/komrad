package ui

import (
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
	return reader.Window{
		Len:     len(d.rows),
		Top:     d.top,
		Markers: d.markers,
	}
}

// columnStyle is the markdown the block beside the list is rendered in.
func columnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
