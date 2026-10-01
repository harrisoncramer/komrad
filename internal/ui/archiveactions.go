package ui

import (
	"slices"

	"komrad/config"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// actions is the archive screen as one table and the legend that names it.
func (a *archiveScreen) actions() screenActionSpec {
	return screenActionSpec{
		table:  a.table(),
		legend: a.legend(),
	}
}

func (a *archiveScreen) table() []screenAction {
	return slices.Concat(
		guarded(a.onAgents, a.agentTable()),
		guarded(a.onSchedules, a.scheduleTable()),
	)
}

// guarded narrows each action to answer only while when holds.
func guarded(when func(panelShared) bool, actions []screenAction) []screenAction {
	return xslice.Map(actions, func(action screenAction) screenAction {
		inner := action.when
		action.when = func(s panelShared) bool { return when(s) && (inner == nil || inner(s)) }

		return action
	})
}

func (a *archiveScreen) scheduleTable() []screenAction {
	list := a.scheduleList()

	return append(list.actions(),
		screenAction{
			id:   actionArchiveBack,
			keys: func(k config.Keybindings) []string { return k.Archive.Back },
			nav:  func(h navHost, _ panelShared, _ string) tea.Cmd { return h.pickView() },
		},
	)
}

func (a *archiveScreen) agentTable() []screenAction {
	list := a.list()

	return append(list.actions(),
		screenAction{
			id:   actionArchiveBack,
			keys: func(k config.Keybindings) []string { return k.Archive.Back },
			when: list.isBrowsing,
			nav:  func(h navHost, _ panelShared, _ string) tea.Cmd { return h.pickView() },
		},
		screenAction{
			id:     actionArchiveRename,
			keys:   func(k config.Keybindings) []string { return k.Archive.Rename },
			when:   list.isBrowsing,
			direct: directly(notInMenu, func(_ host, s panelShared, _ string) tea.Cmd { return a.openRename(s) }),
		},
	)
}

func (a *archiveScreen) legend() []screenLegendRow {
	list := a.list()

	return slices.Concat(list.selectionLegend(), []screenLegendRow{
		{
			of:    []actionID{actionArchiveRename},
			desc:  func(panelShared) string { return "rename" },
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
			desc: func(panelShared) string { return "back" },
		},
		{
			of:    []actionID{actionFilterClear},
			desc:  filterClearDescOf,
			strip: true,
		},
		{
			of:    []actionID{actionFilterMatch},
			desc:  func(panelShared) string { return "next/prev match" },
			strip: true,
		},
	})
}

// holdsAChoosable reports whether the cursor is on an agents row while no selection is active.
func (a *archiveScreen) holdsAChoosable(s panelShared) bool {
	if a.onSchedules(s) || a.selected.isActive() {
		return false
	}

	_, ok := rowAt(a.listRows(s), a.cursor)

	return ok
}

// holdsARenameable reports whether the cursor is on a row that can be renamed.
func (a *archiveScreen) holdsARenameable(s panelShared) bool {
	_, ok := a.renameable(s)

	return ok
}

// holdsAnAgent reports whether the cursor is on an archived agent rather than on the folder
// holding one.
func (a *archiveScreen) holdsAnAgent(s panelShared) bool {
	if a.onSchedules(s) || a.selected.isActive() {
		return false
	}

	_, ok := a.hoveredAgent(s)

	return ok
}

// hoveredAgentID is the archived agent under the cursor, empty on a folder.
func (a *archiveScreen) hoveredAgentID(s panelShared) string {
	agent, ok := a.hoveredAgent(s)
	if !ok {
		return ""
	}

	return agent.ID
}
