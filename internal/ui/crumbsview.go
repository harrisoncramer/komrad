package ui

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
)

func (c crumbs) view(palette paint.Palette, width int) string {
	if len(c.trail) == 0 {
		return ""
	}

	muted := palette.Style(paint.ToneMuted)
	line := muted.Render(strings.Join(c.trail, " "+glyph.Crumb+" "))
	if c.tally != "" {
		line += muted.Render(separated + c.tally)
	}
	if width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line
}
