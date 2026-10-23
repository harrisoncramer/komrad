package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
)

const nameWidth = 30

const separated = " " + glyph.Separator + " "

func clipName(name string) string {
	return paint.Clip(name, nameWidth)
}
