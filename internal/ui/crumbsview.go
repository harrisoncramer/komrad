package ui

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"

	"github.com/charmbracelet/x/ansi"
)

func crumbsView(c screen.Crumbs, palette paint.Palette, width int) string {
	if len(c.Trail) == 0 {
		return ""
	}

	muted := palette.Style(paint.ToneMuted)
	line := muted.Render(strings.Join(c.Trail, " "+glyph.Crumb+" "))
	if c.Tally != "" {
		line += muted.Render(separated + c.Tally)
	}
	if width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line
}
