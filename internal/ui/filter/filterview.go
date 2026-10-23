package filter

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/zone"

	"charm.land/lipgloss/v2"
)

// View renders the search line whether or not there is a search.
func (f State) View(glyphs glyph.Set) string {
	return f.Line(glyphs) + "\n\n"
}

// Line renders the search line on its own.
func (f State) Line(glyphs glyph.Set) string {
	palette := paint.Panel
	before, after := f.Input.Split()
	text := lipgloss.NewStyle()
	if f.IsPatternBroken {
		text = palette.Broken()
	}

	slash := palette.Cursor()
	if !f.IsTyping && !f.IsStanding() {
		slash = paint.Panel.Style(paint.ToneMuted)
	}

	line := slash.Render(f.Mode.Glyph(glyphs)) + " "
	if !f.IsTyping {
		line += text.Render(before + after)
	} else {
		line += palette.RenderTyped(before, after, text)
	}
	if f.problem != "" {
		line += "  " + palette.Broken().Render(f.problem)
	}

	return line
}

// Highlight lights the stretches of a row's name the search's pattern matches.
func (f State) Highlight(palette paint.Palette, name string, base lipgloss.Style) string {
	if f.Matcher == nil || f.IsPatternBroken {
		return base.Render(name)
	}

	return palette.Highlight(name, list.Matches(f.Matcher, name), base)
}

// HeadParams is what the rows over a searched block show besides the search.
type HeadParams struct {
	Glyphs          glyph.Set
	ShouldMarkZones bool
	MatchCount      string
	SaidText        string
	Width           int
}

// Head is the rows over a block the filter searches: the search, what the last key said, and a
// blank row.
func (f State) Head(params HeadParams) []string {
	search := paint.FitRow(f.Line(params.Glyphs)+params.MatchCount, params.Width)
	if params.ShouldMarkZones {
		search = zone.MarkKey(zone.Search, params.Width, 0, zone.SearchBlock) + search
	}

	head := []string{search}
	if params.SaidText != "" {
		head = append(head, paint.FitRow(paint.Panel.Style(paint.ToneMuted).Render(params.SaidText), params.Width))
	}

	return append(head, "")
}
