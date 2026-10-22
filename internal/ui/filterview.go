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

type headParams struct {
	glyphs glyph.Set
	marks  bool
	count  string
	said   string
	width  int
}

// head is the rows over a block the filter searches: the search, what the last key said, and a
// blank row.
func (f filterState) head(params headParams) []string {
	indent := strings.Repeat(" ", docMargin())
	search := fitRow(indent+f.line(params.glyphs)+params.count, params.width)
	if params.marks {
		search = searchMark(stopBlock, params.width, search)
	}

	head := []string{search}
	if params.said != "" {
		head = append(head, fitRow(indent+dimStyle().Render(params.said), params.width))
	}

	return append(head, "")
}

// highlight lights the stretches of a row's name the search's pattern matches.
func (f filterState) highlight(palette paint.Palette, name string, base lipgloss.Style) string {
	if f.matcher == nil || f.broken {
		return base.Render(name)
	}

	return palette.Highlight(name, matchesIn(f.matcher, name), base)
}
