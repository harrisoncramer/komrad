package ui

import (
	"komrad/internal/ui/legend"

	"charm.land/bubbles/v2/key"
)

func renameHelp() legend.Help {
	save := binding(fixedKeys().Submit, "save")
	cancel := binding([]string{"esc"}, "cancel")

	return legend.Help{Short: []key.Binding{save, cancel}, Full: []key.Binding{save, cancel}}
}
