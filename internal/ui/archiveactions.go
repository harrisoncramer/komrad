package ui

import (
	"fmt"

	"weave/config"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// archiveActions is the archive screen as one table, walked in precedence order by the dispatch
// and named in reading order by a legend of the same ids.
func archiveActions() actionSpec {
	return actionSpec{
		table:  archiveTable(),
		legend: archiveLegend(),
	}
}

func archiveTable() []action {
	selecting := func(m model) bool { return m.archive.selected.isActive() }
	browsing := func(m model) bool { return !m.archive.selected.isActive() }

	return []action{
		{
			id:   actionJump,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Top, k.Nav.Bottom) },
			run:  model.jumpArchive,
		},
		{
			id:   actionSelectClear,
			keys: func(k config.Keybindings) []string { return k.Selection.Clear },
			when: selecting,
			run:  model.clearArchiveSelection,
		},
		{
			id:   actionSelectToggle,
			keys: func(k config.Keybindings) []string { return k.Selection.Toggle },
			run:  model.toggleArchiveSelection,
		},
		{
			id:   actionSelectRange,
			keys: func(k config.Keybindings) []string { return k.Selection.Range },
			run:  model.extendArchiveSelection,
		},
		{
			id:   actionRestoreSelected,
			keys: func(k config.Keybindings) []string { return k.Archive.Restore },
			when: selecting,
			run:  model.restoreChosen,
		},
		{
			id:   actionDeleteSelected,
			keys: func(k config.Keybindings) []string { return k.Archive.Delete },
			when: selecting,
			run:  model.deleteChosen,
		},
		{
			id:   actionQuit,
			keys: func(k config.Keybindings) []string { return k.Archive.Quit },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m, m.hide() },
		},
		{
			id:   actionMove,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Up, k.Nav.Down) },
			run:  model.moveArchive,
		},
		{
			id:   actionNavPage,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.PageUp, k.Nav.PageDown) },
			run:  model.pageArchive,
		},
		{
			id:   actionFilterOpen,
			keys: func(k config.Keybindings) []string { return k.Filter.Open },
			when: browsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openFilter() },
		},
		{
			id:   actionFilterMatch,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Filter.NextMatch, k.Filter.PrevMatch) },
			when: func(m model) bool { return !m.archive.selected.isActive() && m.filter.isStanding() },
			run:  model.jumpArchiveMatch,
		},
		{
			id:   actionFilterClear,
			keys: func(k config.Keybindings) []string { return k.Archive.Back },
			when: func(m model) bool { return !m.archive.selected.isActive() && m.filter.isStanding() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.clearSearch() },
		},
		{
			id:   actionArchiveBack,
			keys: func(k config.Keybindings) []string { return k.Archive.Back },
			when: browsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.returnToAgents() },
		},
		{
			id:   actionArchiveRestore,
			keys: func(k config.Keybindings) []string { return k.Archive.Restore },
			when: browsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.restoreFromArchive() },
		},
		{
			id:   actionArchiveRestoreOpen,
			keys: func(k config.Keybindings) []string { return k.Archive.RestoreOpen },
			when: model.canArchiveRowOpen,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.restoreAndOpen() },
		},
		{
			id:   actionArchiveDelete,
			keys: func(k config.Keybindings) []string { return k.Archive.Delete },
			when: browsing,
			run:  model.deleteHovered,
		},
		{
			id:   actionArchiveEmpty,
			keys: func(k config.Keybindings) []string { return k.Archive.Empty },
			when: browsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.emptyArchiveConfirm() },
		},
		{
			id:   actionWorktreesOpen,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Open },
			when: browsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openWorktrees(m.hoveredArchivedAgentID()) },
		},
	}
}

func archiveLegend() []legendRow {
	selecting := func(m model) bool { return m.archive.selected.isActive() }

	return []legendRow{
		{
			of:    []actionID{actionSelectToggle},
			desc:  model.archiveToggleDesc,
			show:  selecting,
			strip: true,
		},
		{
			of:   []actionID{actionSelectRange},
			desc: func(model) string { return "select to nearest" },
			show: selecting,
		},
		{
			of:    []actionID{actionSelectClear},
			desc:  model.archiveClearDesc,
			show:  selecting,
			strip: true,
		},
		{
			of:   []actionID{actionRestoreSelected},
			desc: func(model) string { return "restore all" },
			show: selecting,
		},
		{
			of:   []actionID{actionDeleteSelected},
			desc: func(model) string { return "delete all for good" },
			show: selecting,
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
			desc: model.archiveToggleDesc,
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
			show: func(m model) bool { return !m.archive.selected.isActive() },
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: model.filterDesc,
		},
		{
			of:    []actionID{actionMove},
			desc:  func(model) string { return "move" },
			label: moveLabel,
		},
		{
			of:    []actionID{actionJump},
			desc:  func(model) string { return "top/bottom" },
			label: jumpLabel,
		},
		{
			of:    []actionID{actionNavPage},
			desc:  func(model) string { return "page" },
			label: navPageLabel,
		},
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
	}
}

// doesArchiveHoldARow reports whether the cursor is on something the row keys can act on, which
// an empty archive is not.
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

// jumpArchive answers the bottom key, the top key being a doubled gesture the screen has already
// taken before the table is reached.
func (m model) jumpArchive(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Bottom) {
		return m.jumpArchiveCursor(len(m.archiveRows()) - 1), nil
	}

	return m, nil
}

func (m model) pageArchive(pressed string) (tea.Model, tea.Cmd) {
	return m.jumpArchiveCursor(m.archive.cursor + m.navPageDelta(pressed)), nil
}

func (m model) moveArchive(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Down) {
		return m.moveArchiveCursor(1), nil
	}

	return m.moveArchiveCursor(-1), nil
}

func (m model) jumpArchiveMatch(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Filter.PrevMatch) {
		return m.jumpMatch(-1)
	}

	return m.jumpMatch(1)
}

func (m model) clearArchiveSelection(string) (tea.Model, tea.Cmd) {
	m.archive.selected = selection{}

	return m, nil
}

func (m model) toggleArchiveSelection(string) (tea.Model, tea.Cmd) {
	m.archive.selected = m.archive.selected.toggle(refsOf(m.archiveRows()), m.archive.cursor)

	return m, nil
}

func (m model) extendArchiveSelection(string) (tea.Model, tea.Cmd) {
	if !m.archive.selected.isActive() {
		return m.toggleArchiveSelection("")
	}

	m.archive.selected = m.archive.selected.extend(refsOf(m.archiveRows()), m.archive.cursor)

	return m, nil
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

func (m model) archiveToggleDesc() string {
	hovered, _ := rowAt(m.archiveRows(), m.archive.cursor)

	switch {
	case hovered.header:
		return "select folder"
	case m.archive.selected.has(rowKey(hovered)):
		return "deselect"
	}

	return "select"
}

func (m model) archiveClearDesc() string {
	return fmt.Sprintf("clear %d selected", len(m.archive.selected.keys))
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
