package ui

import (
	"komrad/internal/ui/keys"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/tabs"

	"github.com/charmbracelet/x/ansi"
)

func (m *model) kindsLine() string {
	k, _ := m.activeKinds()
	row := k.Kinds()
	row.MarkStop = m.spanMarker(zoneKinds, row.Labels)
	if *k.KindsFocused() {
		row.FocusedStop, row.ChosenStop = row.ChosenStop, tabs.NoStop
	}

	line := paint.Panel.Style(paint.ToneMuted).Render(keys.HeadLabel(m.keys.Kinds.Focus)) + "   " + row.View()
	if width := m.paneWidth(); width > 0 {
		line = ansi.Truncate(line, width, "…")
	}

	return line + "\n"
}
