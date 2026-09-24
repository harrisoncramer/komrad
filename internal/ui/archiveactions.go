package ui

import (
	"slices"

	"komrade/config"
	"komrade/internal/ui/panel"

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
			id:   actionArchiveEmpty,
			keys: func(k config.Keybindings) []string { return k.Archive.Empty },
			when: list.isBrowsing,
			run:  func(h host, s panelShared, _ string) tea.Cmd { return confirmEmptyArchive(h, s) },
		},
		screenAction{
			id:   actionWorktreesOpen,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Open },
			when: list.isBrowsing,
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return h.enter(screenWorktrees, screenTarget{agentID: a.hoveredAgentID(s)})
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
			of:   []actionID{actionArchiveRestoreOpen},
			desc: func(panelShared) string { return "restore and open" },
			show: a.canRowOpen,
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: list.toggleDesc,
			show: a.holdsARow,
		},
		{
			of:   []actionID{actionArchiveDelete},
			desc: func(panelShared) string { return "delete for good" },
			show: a.holdsAnAgent,
		},
		{
			of:   []actionID{actionArchiveEmpty},
			desc: func(panelShared) string { return "empty the archive" },
			show: a.holdsAnAgent,
		},
		{
			of:   []actionID{actionWorktreesOpen},
			desc: func(panelShared) string { return "worktrees" },
			show: list.isBrowsing,
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

// holdsARow reports whether the cursor is on something the row keys can act on.
func (a *archiveScreen) holdsARow(s panelShared) bool {
	if _, ok := a.hoveredWorkflow(s); ok {
		return !a.selected.isActive()
	}

	return a.holdsAnAgent(s)
}

// holdsAnAgent reports whether the cursor is on an archived agent rather than on the folder
// holding one.
func (a *archiveScreen) holdsAnAgent(s panelShared) bool {
	if a.selected.isActive() {
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
	if _, ok := a.hoveredWorkflow(s); ok {
		return "restore workflow"
	}

	return "restore"
}

// canRowOpen reports whether the row under the cursor holds a conversation the panel can put
// back in a pane.
func (a *archiveScreen) canRowOpen(s panelShared) bool {
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
