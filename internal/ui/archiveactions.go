package ui

import (
	"slices"

	"weave/config"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// archiveActions is the archive screen as one table and the legend that names it.
func archiveActions() actionSpec {
	return actionSpec{
		table:  archiveTable(),
		legend: archiveLegend(),
	}
}

func archiveTable() []action {
	list := archiveList()

	return append(list.actions(),
		action{
			id:   actionRestoreSelected,
			keys: func(k config.Keybindings) []string { return k.Archive.Restore },
			when: list.isSelecting,
			run:  model.restoreChosen,
		},
		action{
			id:   actionDeleteSelected,
			keys: func(k config.Keybindings) []string { return k.Archive.Delete },
			when: list.isSelecting,
			run:  model.deleteChosen,
		},
		action{
			id:   actionArchiveBack,
			keys: func(k config.Keybindings) []string { return k.Archive.Back },
			when: list.isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.returnToAgents() },
		},
		action{
			id:   actionArchiveRestore,
			keys: func(k config.Keybindings) []string { return k.Archive.Restore },
			when: list.isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.restoreFromArchive() },
		},
		action{
			id:   actionArchiveRestoreOpen,
			keys: func(k config.Keybindings) []string { return k.Archive.RestoreOpen },
			when: model.canArchiveRowOpen,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.restoreAndOpen() },
		},
		action{
			id:   actionArchiveDelete,
			keys: func(k config.Keybindings) []string { return k.Archive.Delete },
			when: list.isBrowsing,
			run:  model.deleteHovered,
		},
		action{
			id:   actionArchiveEmpty,
			keys: func(k config.Keybindings) []string { return k.Archive.Empty },
			when: list.isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.emptyArchiveConfirm() },
		},
		action{
			id:   actionWorktreesOpen,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Open },
			when: list.isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openWorktrees(m.hoveredArchivedAgentID()) },
		},
	)
}

func archiveLegend() []legendRow {
	list := archiveList()

	return slices.Concat(list.selectionLegend(), []legendRow{
		{
			of:   []actionID{actionRestoreSelected},
			desc: func(model) string { return "restore all" },
			show: list.isSelecting,
		},
		{
			of:   []actionID{actionDeleteSelected},
			desc: func(model) string { return "delete all for good" },
			show: list.isSelecting,
		},
		{
			of:    []actionID{actionArchiveRestore},
			desc:  model.archiveRestoreDesc,
			show:  model.doesArchiveHoldARow,
			strip: true,
		},
		{
			of:   []actionID{actionArchiveRestoreOpen},
			desc: func(model) string { return "restore and open" },
			show: model.canArchiveRowOpen,
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: list.toggleDesc,
			show: model.doesArchiveHoldARow,
		},
		{
			of:   []actionID{actionArchiveDelete},
			desc: func(model) string { return "delete for good" },
			show: model.doesArchiveHoldAnAgent,
		},
		{
			of:   []actionID{actionArchiveEmpty},
			desc: func(model) string { return "empty the archive" },
			show: model.doesArchiveHoldAnAgent,
		},
		{
			of:   []actionID{actionWorktreesOpen},
			desc: func(model) string { return "worktrees" },
			show: list.isBrowsing,
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: model.filterDesc,
		},
	}, navLegend(), []legendRow{
		{
			of:   []actionID{actionArchiveBack},
			desc: func(model) string { return "back" },
		},
		{
			of:    []actionID{actionFilterClear},
			desc:  model.filterClearDesc,
			strip: true,
		},
		{
			of:    []actionID{actionFilterMatch},
			desc:  func(model) string { return "next/prev match" },
			strip: true,
		},
	})
}

// doesArchiveHoldARow reports whether the cursor is on something the row keys can act on.
func (m model) doesArchiveHoldARow() bool {
	if _, ok := m.hoveredArchivedWorkflow(); ok {
		return !m.archive.selected.isActive()
	}

	return m.doesArchiveHoldAnAgent()
}

// doesArchiveHoldAnAgent reports whether the cursor is on an archived agent rather than on the
// folder holding one.
func (m model) doesArchiveHoldAnAgent() bool {
	if m.archive.selected.isActive() {
		return false
	}

	_, ok := m.selectedArchived()

	return ok
}

func (m model) restoreChosen(string) (tea.Model, tea.Cmd) {
	return m.restoreAgents(chosenAgents(m.archive.selected, m.archiveRows()))
}

func (m model) deleteChosen(string) (tea.Model, tea.Cmd) {
	return m.deleteFromArchiveConfirm(chosenAgents(m.archive.selected, m.archiveRows()))
}

func (m model) deleteHovered(string) (tea.Model, tea.Cmd) {
	if workflow, ok := m.hoveredArchivedWorkflow(); ok {
		return m.deleteWorkflowConfirm(workflow)
	}

	agent, ok := m.selectedArchived()
	if !ok {
		return m, nil
	}

	return m.deleteFromArchiveConfirm([]agentstore.Agent{agent})
}

func (m model) archiveRestoreDesc() string {
	if _, ok := m.hoveredArchivedWorkflow(); ok {
		return "restore workflow"
	}

	return "restore"
}

// canArchiveRowOpen reports whether the row under the cursor holds a conversation the panel can
// put back in a pane.
func (m model) canArchiveRowOpen() bool {
	agent, ok := m.selectedArchived()

	return ok && agent.State.DoesPaneDecide()
}

// hoveredArchivedAgentID is the archived agent under the cursor, empty on a folder.
func (m model) hoveredArchivedAgentID() string {
	agent, ok := m.selectedArchived()
	if !ok {
		return ""
	}

	return agent.ID
}
