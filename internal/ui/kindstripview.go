package ui

import (
	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
)

func (m *model) kindsLine() string {
	k, _ := m.activeKinds()
	row := k.kinds(m)
	row.mark = m.spanMarker(zoneKinds, row.labels)
	if *k.kindsFocused(m) {
		row.focus, row.lit = row.lit, noStop
	}

	line := paint.Panel.Style(paint.ToneMuted).Render(headLabel(m.keys.Kinds.Focus)) + "   " + row.view()
	if width := m.paneWidth(); width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line + "\n"
}
