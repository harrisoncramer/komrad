package ui

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
)

// view renders the search line whether or not there is a search.
func (f filterState) view(glyphs glyph.Set) string {
	return f.line(glyphs) + "\n\n"
}

func (f filterState) line(glyphs glyph.Set) string {
	palette := paint.Panel
	before, after := f.input.Split()
	text := lipgloss.NewStyle()
	if f.broken {
		text = palette.Broken()
	}

	slash := palette.Cursor()
	if !f.typing && !f.isStanding() {
		slash = dimStyle()
	}

	line := slash.Render(f.mode.glyph(glyphs)) + " "
	if !f.typing {
		line += text.Render(before + after)
	} else {
		line += palette.RenderTyped(before, after, text)
	}
	if f.problem != "" {
		line += "  " + palette.Broken().Render(f.problem)
	}

	return line
}

// head is the rows over a block the filter searches: the search, what the last key said, and a
// blank row.
func (f filterState) head(m *model, count, said string, width int) []string {
	indent := strings.Repeat(" ", docMargin())
	head := []string{m.markSearch(stopBlock, width, fitRow(indent+f.line(m.glyphs)+count, width))}
	if said != "" {
		head = append(head, fitRow(indent+dimStyle().Render(said), width))
	}

	return append(head, "")
}

// highlight lights the stretches of a row's name the search's pattern matches.
func (f filterState) highlight(palette paint.Palette, name string, base lipgloss.Style) string {
	if f.pattern == nil || f.broken {
		return base.Render(name)
	}

	return palette.Highlight(name, matchesIn(f.pattern, name), base)
}
