package filter

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"

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
	if f.Broken {
		text = palette.Broken()
	}

	slash := palette.Cursor()
	if !f.Typing && !f.IsStanding() {
		slash = paint.Panel.Style(paint.ToneMuted)
	}

	line := slash.Render(f.Mode.Glyph(glyphs)) + " "
	if !f.Typing {
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
	if f.Matcher == nil || f.Broken {
		return base.Render(name)
	}

	return palette.Highlight(name, list.Matches(f.Matcher, name), base)
}
