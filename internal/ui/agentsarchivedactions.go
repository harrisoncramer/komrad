package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
)

// actions is the archived agents as one table and the legend that names it.
func (a *archivedAgents) actions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table:  a.table(),
		Legend: a.legend(),
	}
}

func (a *archivedAgents) table() []keys.Action[*model] {
	list := a.list()

	return append(list.actions(),
		keys.Action[*model]{
			ID:   actionArchiveBack,
			Keys: keys.FixedBack,
			When: list.isBrowsing,
			Nav:  func(m *model, _ string) screen.Request { return screen.PickView{} },
		},
		keys.Action[*model]{
			ID:     actionArchiveRename,
			Keys:   func(k config.Keybindings) []string { return k.Archive.Rename },
			When:   list.isBrowsing,
			Direct: keys.Directly(keys.NotInMenu, func(m *model, _ string) screen.Request { return a.openRename(m) }),
		},
	)
}

func (a *archivedAgents) legend() []keys.LegendRow[*model] {
	list := a.list()

	return slices.Concat(list.selectionLegend(), []keys.LegendRow[*model]{
		{
			Of:    []keys.ID{actionArchiveRename},
			Desc:  func(*model) string { return "rename" },
			Show:  a.holdsARenameable,
			Strip: true,
		},
		{
			Of:   []keys.ID{keys.SelectToggle},
			Desc: list.toggleDesc,
			Show: a.holdsAChoosable,
		},
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf,
		},
	}, keys.NavLegend[*model](), []keys.LegendRow[*model]{
		{
			Of:   []keys.ID{actionArchiveBack},
			Desc: func(*model) string { return "back" },
		},
		{
			Of:    []keys.ID{keys.FilterClear},
			Desc:  filterClearDescOf,
			Strip: true,
		},
		{
			Of:    []keys.ID{keys.FilterMatch},
			Desc:  func(*model) string { return "next/prev match" },
			Strip: true,
		},
	})
}

// holdsAChoosable reports whether the cursor is on a row while no selection is active.
func (a *archivedAgents) holdsAChoosable(m *model) bool {
	if a.Selected.IsActive() {
		return false
	}

	_, ok := list.At(a.listRows(m), a.Row)

	return ok
}

// holdsARenameable reports whether the cursor sits on a row the user can rename.
func (a *archivedAgents) holdsARenameable(m *model) bool {
	_, ok := a.renameable(m)

	return ok
}

// holdsAnAgent reports whether the cursor is on an archived agent rather than on the folder
// holding one.
func (a *archivedAgents) holdsAnAgent(m *model) bool {
	if a.Selected.IsActive() {
		return false
	}

	_, ok := a.hoveredAgent(m)

	return ok
}

// hoveredAgentID is the archived agent under the cursor, empty on a folder.
func (a *archivedAgents) hoveredAgentID(m *model) string {
	agent, ok := a.hoveredAgent(m)
	if !ok {
		return ""
	}

	return agent.ID
}
