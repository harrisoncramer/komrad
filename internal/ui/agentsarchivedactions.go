package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the archived agents as one table and the legend that names it.
func (a *archivedAgents) actions() screenActionSpec {
	return screenActionSpec{
		table:  a.table(),
		legend: a.legend(),
	}
}

func (a *archivedAgents) table() []screenAction {
	list := a.list()

	return append(list.actions(),
		screenAction{
			id:   actionArchiveBack,
			keys: fixedBack,
			when: list.isBrowsing,
			nav:  func(m *model, _ string) tea.Cmd { return m.pickView() },
		},
		screenAction{
			id:     actionArchiveRename,
			keys:   func(k config.Keybindings) []string { return k.Archive.Rename },
			when:   list.isBrowsing,
			direct: directly(notInMenu, func(m *model, _ string) tea.Cmd { return a.openRename(m) }),
		},
	)
}

func (a *archivedAgents) legend() []screenLegendRow {
	list := a.list()

	return slices.Concat(list.selectionLegend(), []screenLegendRow{
		{
			of:    []actionID{actionArchiveRename},
			desc:  func(*model) string { return "rename" },
			show:  a.holdsARenameable,
			strip: true,
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: list.toggleDesc,
			show: a.holdsAChoosable,
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: filterDescOf,
		},
	}, screenNavLegend(), []screenLegendRow{
		{
			of:   []actionID{actionArchiveBack},
			desc: func(*model) string { return "back" },
		},
		{
			of:    []actionID{actionFilterClear},
			desc:  filterClearDescOf,
			strip: true,
		},
		{
			of:    []actionID{actionFilterMatch},
			desc:  func(*model) string { return "next/prev match" },
			strip: true,
		},
	})
}

// holdsAChoosable reports whether the cursor is on a row while no selection is active.
func (a *archivedAgents) holdsAChoosable(m *model) bool {
	if a.selected.isActive() {
		return false
	}

	_, ok := rowAt(a.listRows(m), a.cursor)

	return ok
}

// holdsARenameable reports whether the cursor is on a row that can be renamed.
func (a *archivedAgents) holdsARenameable(m *model) bool {
	_, ok := a.renameable(m)

	return ok
}

// holdsAnAgent reports whether the cursor is on an archived agent rather than on the folder
// holding one.
func (a *archivedAgents) holdsAnAgent(m *model) bool {
	if a.selected.isActive() {
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
