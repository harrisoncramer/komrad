package ui

import (
	"strings"

	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderInMode is the sidebar inside its gutters with label rendered in tone at the right end of the
// sidebar's bottom row.
func (m *model) renderInMode(tone paint.Tone, label string) string {
	lines := strings.Split(m.renderInset(m.insideView()), "\n")

	bottom := len(lines) - 1
	if m.term.height > 0 {
		bottom = min(bottom, m.term.height-1)
	}

	lines[bottom] = stampMode(lines[bottom], m.findModeEnd(), tone, label)

	return strings.Join(lines, "\n")
}

// stampMode renders label in tone over row, ending at the column end.
func stampMode(row string, end int, tone paint.Tone, label string) string {
	badge := paint.Panel.Mode(tone).Render(label)
	at := end - lipgloss.Width(badge)
	if at < 0 || end > ansi.StringWidth(row) {
		return row
	}

	return ansi.Truncate(row, at, "") + badge + ansi.Cut(row, end, ansi.StringWidth(row))
}
