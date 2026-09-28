package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/panel"
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
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.enter(screenAgents, screenTarget{}) },
		},
		screenAction{
			id:   actionArchiveRestore,
			keys: func(k config.Keybindings) []string { return k.Archive.Restore },
			run:  func(h host, s panelShared, _ string) tea.Cmd { return a.restoreSchedule(h, s) },
		},
		screenAction{
			id:   actionArchiveDelete,
			keys: func(k config.Keybindings) []string { return k.Archive.Delete },
			run:  func(h host, s panelShared, _ string) tea.Cmd { return a.confirmDeleteSchedule(h, s) },
		},
	)
}

func (a *archiveScreen) agentTable() []screenAction {
	list := a.list()

	return append(list.actions(),
		screenAction{
			id:   actionRestoreSelected,
			keys: func(k config.Keybindings) []string { return k.Archive.Restore },
			when: list.isSelecting,
			run:  a.restoreChosen,
		},
		screenAction{
			id:   actionDeleteSelected,
			keys: func(k config.Keybindings) []string { return k.Archive.Delete },
			when: list.isSelecting,
			run:  a.deleteChosen,
		},
		screenAction{
			id:   actionArchiveBack,
			keys: func(k config.Keybindings) []string { return k.Archive.Back },
			when: list.isBrowsing,
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.enter(screenAgents, screenTarget{}) },
		},
		screenAction{
			id:   actionArchiveRestore,
			keys: func(k config.Keybindings) []string { return k.Archive.Restore },
			when: list.isBrowsing,
			run:  func(h host, s panelShared, _ string) tea.Cmd { return a.restore(h, s) },
		},
		screenAction{
			id:   actionArchiveRestoreOpen,
			keys: func(k config.Keybindings) []string { return k.Archive.RestoreOpen },
			when: a.canRowOpen,
			run:  func(h host, s panelShared, _ string) tea.Cmd { return a.restoreAndOpen(h, s) },
		},
		screenAction{
			id:   actionArchiveDelete,
			keys: func(k config.Keybindings) []string { return k.Archive.Delete },
			when: list.isBrowsing,
			run:  a.deleteHovered,
		},
		screenAction{
			id:   actionPlacesOpen,
			keys: func(k config.Keybindings) []string { return k.Places.Open },
			when: a.holdsAnAgent,
			run: func(h host, s panelShared, _ string) tea.Cmd {
				row, ok := rowAt(a.listRows(s), a.cursor)
				if !ok || row.header {
					return nil
				}

				return openPlaces(h, s, screenArchive, row.item)
			},
		},
	)
}

func (a *archiveScreen) legend() []screenLegendRow {
	list := a.list()

	return slices.Concat(list.selectionLegend(), []screenLegendRow{
		{
			of:   []actionID{actionRestoreSelected},
			desc: func(panelShared) string { return "restore all" },
			show: list.isSelecting,
		},
		{
			of:   []actionID{actionDeleteSelected},
			desc: func(panelShared) string { return "delete all for good" },
			show: list.isSelecting,
		},
		{
			of:    []actionID{actionArchiveRestore},
			desc:  a.restoreDesc,
			show:  a.holdsARow,
			strip: true,
		},
		{
			of:   []actionID{actionArchiveDelete},
			desc: func(panelShared) string { return "delete for good" },
			show: a.holdsASchedule,
		},
		{
			of:   []actionID{actionArchiveRestoreOpen},
			desc: func(panelShared) string { return "restore and open" },
			show: a.canRowOpen,
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: list.toggleDesc,
			show: func(s panelShared) bool { return a.onAgents(s) && a.holdsARow(s) },
		},
		{
			of:   []actionID{actionArchiveDelete},
			desc: func(panelShared) string { return "delete for good" },
			show: a.holdsAnAgent,
		},
		{
			of:   []actionID{actionPlacesOpen},
			desc: func(panelShared) string { return "places" },
			show: a.holdsAnAgent,
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

// holdsASchedule reports whether the archive lists schedules and the cursor is on one.
func (a *archiveScreen) holdsASchedule(s panelShared) bool {
	if !a.onSchedules(s) {
		return false
	}

	_, ok := a.hoveredSchedule(s)

	return ok
}

// holdsARow reports whether the cursor is on something the row keys can act on.
func (a *archiveScreen) holdsARow(s panelShared) bool {
	if a.onSchedules(s) {
		return a.holdsASchedule(s)
	}

	if _, ok := a.hoveredWorkflow(s); ok {
		return !a.selected.isActive()
	}

	return a.holdsAnAgent(s)
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

func (a *archiveScreen) restoreChosen(h host, s panelShared, _ string) tea.Cmd {
	return restoreAgentsCmd(h, chosenAgents(a.selected, a.listRows(s)))
}

func (a *archiveScreen) deleteChosen(h host, s panelShared, _ string) tea.Cmd {
	return confirmDeleteArchived(h, s, chosenAgents(a.selected, a.listRows(s)))
}

func (a *archiveScreen) deleteHovered(h host, s panelShared, _ string) tea.Cmd {
	if workflow, ok := a.hoveredWorkflow(s); ok {
		return confirmDeleteWorkflow(h, s, workflow)
	}

	agent, ok := a.hoveredAgent(s)
	if !ok {
		return nil
	}

	return confirmDeleteArchived(h, s, []panel.Agent{agent})
}

func (a *archiveScreen) restoreDesc(s panelShared) string {
	if a.onSchedules(s) {
		return "restore schedule"
	}

	if _, ok := a.hoveredWorkflow(s); ok {
		return "restore workflow"
	}

	return "restore"
}

// canRowOpen reports whether the row under the cursor holds a conversation the panel can put
// back in a pane.
func (a *archiveScreen) canRowOpen(s panelShared) bool {
	if a.onSchedules(s) {
		return false
	}

	agent, ok := a.hoveredAgent(s)

	return ok && agent.State.DoesPaneDecide()
}

// hoveredAgentID is the archived agent under the cursor, empty on a folder.
func (a *archiveScreen) hoveredAgentID(s panelShared) string {
	agent, ok := a.hoveredAgent(s)
	if !ok {
		return ""
	}

	return agent.ID
}
