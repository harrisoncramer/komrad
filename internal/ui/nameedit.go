package ui

import (
	"slices"

	"komrad/internal/ui/field"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"

	"charm.land/bubbles/v2/key"
)

// reconcileNameEdit says whether a field stays open over rows whose cursor is on hovered.
func reconcileNameEdit[T list.Keyed](e *field.NameEdit, rows []T, hovered string, name func(T) string) bool {
	at := slices.IndexFunc(rows, func(r T) bool { return r.Key() == e.Key() })
	if at < 0 {
		return e.Reconcile(hovered, "", false)
	}

	return e.Reconcile(hovered, name(rows[at]), true)
}

func renameHelp() legend.Help {
	save := binding(fixedKeys().Submit, "save")
	cancel := binding([]string{"esc"}, "cancel")

	return legend.Help{Short: []key.Binding{save, cancel}, Full: []key.Binding{save, cancel}}
}
