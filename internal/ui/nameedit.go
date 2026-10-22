package ui

import (
	"slices"

	"komrad/internal/ui/field"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"

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

// renameSettled is a rename landed, for the screens that typed it.
type renameSettled struct {
	row string
	err error
}

// renameCmd sends a name typed over a row, handing the outcome to the screens that type names.
func (m *model) renameCmd(doing, summary, row string, write func(api) error) screen.Request {
	clients := m.api

	return screen.Write{
		Doing:   doing,
		Summary: summary,
		Send: func() screen.Landed {
			err := write(clients)

			return screen.Landed{Err: err, Then: renameSettled{row: row, err: err}}
		},
	}
}

// settleRename marks a typed name saved once its write has landed, or open for another try when it failed.
func (m *model) settleRename(settled renameSettled) {
	m.agents.settleRename(m, settled.row, settled.err)
	m.documents.settleRename(m, settled.row, settled.err)
	m.agents.archived.settleRename(m, settled.row, settled.err)
	m.agents.deleted.settleRename(m, settled.row, settled.err)
	m.schedules.settleRename(m, settled.row, settled.err)
}

func renameHelp() keyMap {
	save := binding(fixedKeys().Submit, "save")
	cancel := binding([]string{"esc"}, "cancel")

	return keyMap{short: []key.Binding{save, cancel}, full: []key.Binding{save, cancel}}
}
