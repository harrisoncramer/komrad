package ui

import (
	"math"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
)

// engineLine is the engine rendered in at most width cells, dropping the commit before the name.
func (m *model) engineLine(width int) string {
	muted := paint.Panel.Style(paint.ToneMuted)
	line := paint.Panel.Style(m.engineTone()).Render(m.glyphs.Spell(glyph.Engine)) + " " + muted.Render(engineTitle(m.engineStatus.Name))

	room := width - ansi.StringWidth(line) - 2
	if width <= 0 {
		room = math.MaxInt
	}

	if flash, ok := m.renderMessagesFlash(room); ok {
		return line + "  " + m.markSpan(zoneMessages, "", flash)
	}

	tally := m.renderMessagesTally()
	if tally != "" {
		tally = "  " + m.markSpan(zoneMessages, "", tally)
	}

	if m.engineBuild.Commit == "" {
		return line + tally
	}

	withCommit := line + " " + muted.Render(m.engineBuild.Commit)
	if width > 0 && ansi.StringWidth(withCommit+tally) > width {
		return line + tally
	}

	return withCommit + tally
}
