package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// actions is the archived agents as one table and the legend that names it.
func (a *archivedAgents) actions(in screen.Inputs) keys.Spec[*archivedAgents] {
	return keys.Spec[*archivedAgents]{
		Table:  a.table(in),
		Legend: a.legend(in),
	}
}

func (a *archivedAgents) table(in screen.Inputs) []keys.Action[*archivedAgents] {
	list := a.list()
	browsing := func(*archivedAgents) bool { return list.isBrowsing() }

	return append(listActions[*archivedAgents](list, in),
		keys.Action[*archivedAgents]{
			ID:   actionArchiveBack,
			Keys: keys.FixedBack,
			When: browsing,
			Nav:  func(*archivedAgents, string) screen.Request { return screen.PickView{} },
		},
		keys.Action[*archivedAgents]{
			ID:     actionArchiveRename,
			Keys:   func(k config.Keybindings) []string { return k.Archive.Rename },
			When:   browsing,
			Direct: keys.Directly(keys.NotInMenu, func(*archivedAgents, string) screen.Request { return a.openRename(in) }),
		},
	)
}

func (a *archivedAgents) legend(in screen.Inputs) []keys.LegendRow[*archivedAgents] {
	list := a.list()

	return slices.Concat(selectionLegend[*archivedAgents](list, in), []keys.LegendRow[*archivedAgents]{
		{
			Of:    []keys.ID{actionArchiveRename},
			Desc:  func(*archivedAgents) string { return "rename" },
			Show:  func(*archivedAgents) bool { return a.holdsARenameable(in) },
			Strip: true,
		},
		{
			Of:   []keys.ID{keys.SelectToggle},
			Desc: func(*archivedAgents) string { return list.toggleDesc(in) },
			Show: func(*archivedAgents) bool { return a.holdsAChoosable(in) },
		},
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf[*archivedAgents](in),
		},
	}, keys.NavLegend[*archivedAgents](), []keys.LegendRow[*archivedAgents]{
		{
			Of:   []keys.ID{actionArchiveBack},
			Desc: func(*archivedAgents) string { return "back" },
		},
		{
			Of:    []keys.ID{keys.FilterClear},
			Desc:  filterClearDescOf[*archivedAgents](in),
			Strip: true,
		},
		{
			Of:    []keys.ID{keys.FilterMatch},
			Desc:  func(*archivedAgents) string { return "next/prev match" },
			Strip: true,
		},
	})
}

// holdsAChoosable reports whether the cursor is on a row while no selection is active.
func (a *archivedAgents) holdsAChoosable(in screen.Inputs) bool {
	if a.Selected.IsActive() {
		return false
	}

	_, ok := list.At(a.listRows(in), a.Row)

	return ok
}

// holdsARenameable reports whether the cursor sits on a row the user can rename.
func (a *archivedAgents) holdsARenameable(in screen.Inputs) bool {
	_, ok := a.renameable(in)

	return ok
}

// holdsAnAgent reports whether the cursor is on an archived agent rather than on the folder
// holding one.
func (a *archivedAgents) holdsAnAgent(in screen.Inputs) bool {
	if a.Selected.IsActive() {
		return false
	}

	_, ok := a.hoveredAgent(in)

	return ok
}

// HoveredAgentID is the archived agent under the cursor, empty on a folder.
func (a *archivedAgents) HoveredAgentID(in screen.Inputs) string {
	agent, ok := a.hoveredAgent(in)
	if !ok {
		return ""
	}

	return agent.ID
}

// Resting is never an agent on the archive.
func (a *archivedAgents) Resting(screen.Inputs) (panel.AgentRow, bool) {
	return panel.AgentRow{}, false
}
