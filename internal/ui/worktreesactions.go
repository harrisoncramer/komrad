package ui

import (
	"slices"

	"weave/config"
	agentstore "weave/internal/store/agent"
	"weave/internal/ui/paint/label"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
)

// worktreesActions is the worktrees screen as one table and the legend that names it.
func worktreesActions() actionSpec {
	return actionSpec{
		table:  worktreesTable(),
		legend: worktreesLegend(),
	}
}

func worktreesTable() []action {
	list := worktreesList()

	return append(list.actions(),
		action{
			id:   actionDeleteSelected,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Delete },
			when: list.isSelecting,
			run:  model.deleteChosenWorktrees,
		},
		action{
			id:   actionWorktreesBack,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Back },
			when: list.isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.returnToAgents() },
		},
		action{
			id:   actionWorktreesJumpToAgent,
			keys: func(k config.Keybindings) []string { return k.Worktrees.JumpToAgent },
			when: model.canWorktreeRowOpen,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.jumpToWorktreeAgent() },
		},
		action{
			id:   actionWorktreesDelete,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Delete },
			when: list.isBrowsing,
			run:  model.deleteHoveredWorktree,
		},
		action{
			id:   actionWorktreesClear,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Clear },
			when: list.isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.clearWorktreesConfirm() },
		},
	)
}

func worktreesLegend() []legendRow {
	list := worktreesList()

	return slices.Concat(list.selectionLegend(), []legendRow{
		{
			of:   []actionID{actionDeleteSelected},
			desc: func(model) string { return "delete all" },
			show: list.isSelecting,
		},
		{
			of:    []actionID{actionWorktreesJumpToAgent},
			desc:  func(model) string { return "jump to agent" },
			show:  model.canWorktreeRowOpen,
			strip: true,
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: list.toggleDesc,
			show: model.doesWorktreesHoldARow,
		},
		{
			of:    []actionID{actionWorktreesDelete},
			desc:  model.worktreeDeleteDesc,
			show:  model.doesWorktreesHoldADirectory,
			strip: true,
		},
		{
			of:   []actionID{actionWorktreesClear},
			desc: func(model) string { return "clear what can go" },
			show: model.doesWorktreesHoldARow,
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: model.filterDesc,
		},
	}, navLegend(), []legendRow{
		{
			of:   []actionID{actionWorktreesBack},
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

func (m model) doesWorktreesHoldARow() bool {
	if m.worktrees.selected.isActive() {
		return false
	}

	_, ok := rowAt(m.worktreeRows(), m.worktrees.cursor)

	return ok
}

// doesWorktreesHoldADirectory reports whether the cursor is on a directory rather than on a
// repository.
func (m model) doesWorktreesHoldADirectory() bool {
	if m.worktrees.selected.isActive() {
		return false
	}

	_, ok := m.hoveredWorktree()

	return ok
}

// canWorktreeRowOpen reports whether the hovered row has an agent the panel can land on.
func (m model) canWorktreeRowOpen() bool {
	if m.worktrees.selected.isActive() {
		return false
	}

	row, ok := m.hoveredWorktree()

	return ok && row.item.agent != nil
}

// worktreeDeleteDesc names what the delete key does to the hovered directory.
func (m model) worktreeDeleteDesc() string {
	row, ok := m.hoveredWorktree()
	if !ok {
		return "delete"
	}

	switch m.worktreeFate(row.item) {
	case fateLive:
		return "archive the agent"
	case fateArchived:
		return "delete the agent"
	case fateUnowned:
		return "remove"
	}

	return ""
}

// worktreeFate is which of the three delete paths a directory takes.
type worktreeFate int

const (
	// fateUnowned is a directory nothing answers for, which the remover takes.
	fateUnowned worktreeFate = iota
	// fateArchived is a directory whose agent is put away, which the agent delete takes.
	fateArchived
	// fateLive is a directory whose agent is running, which is offered the archive.
	fateLive
)

func (m model) worktreeFate(item worktreeItem) worktreeFate {
	switch {
	case item.agent == nil:
		return fateUnowned
	case item.agent.state == agentstore.StateFailedToCleanup && !item.readable:
		return fateUnowned
	case item.agent.archivedAt != nil:
		return fateArchived
	}

	return fateLive
}

// jumpToWorktreeAgent lands the agents list or the archive on the hovered row's agent.
func (m model) jumpToWorktreeAgent() (tea.Model, tea.Cmd) {
	row, ok := m.hoveredWorktree()
	if !ok || row.item.agent == nil {
		return m, nil
	}

	id := row.item.agent.id
	if _, live := m.liveAgent(id); live {
		m.enterScreen(screenAgents)
		m.revealAgent(id)
		m.stageSelection()

		return m, nil
	}

	m.enterScreen(screenArchive)

	m.archive.focus = id
	if idx := rowIndexOfAgentID(m.archiveRows(), id); idx >= 0 {
		m.archive.cursor = idx
	}

	return m.enterArchive()
}

func (m model) deleteHoveredWorktree(string) (tea.Model, tea.Cmd) {
	row, ok := m.hoveredWorktree()
	if !ok {
		return m, nil
	}

	switch m.worktreeFate(row.item) {
	case fateLive:
		if item, ok := m.liveAgent(row.item.agent.id); ok {
			return m.archiveAgentsConfirm([]agentstore.Agent{item.agent})
		}
	case fateArchived:
		return m.deleteWorktreesConfirm(worktreeDeletion{agents: []worktreeItem{row.item}})
	case fateUnowned:
		return m.deleteWorktreesConfirm(worktreeDeletion{unowned: []worktreeItem{row.item}})
	}

	return m, nil
}

func (m model) deleteChosenWorktrees(string) (tea.Model, tea.Cmd) {
	deletion := worktreeDeletion{}
	for _, row := range m.worktreeRows() {
		if row.header || !m.worktrees.selected.has(row.key()) {
			continue
		}

		switch m.worktreeFate(row.item) {
		case fateLive:
			m.reportBrief("A live agent is in the selection", "Archive "+row.name()+" from the agents list first; nothing bulk stops a running agent.", nil)

			return m, nil
		case fateArchived:
			deletion.agents = append(deletion.agents, row.item)
		case fateUnowned:
			deletion.unowned = append(deletion.unowned, row.item)
		}
	}

	return m.deleteWorktreesConfirm(deletion)
}

// clearWorktreesConfirm offers everything on the screen that can go without stopping an agent.
func (m model) clearWorktreesConfirm() (tea.Model, tea.Cmd) {
	deletion := worktreeDeletion{}
	for _, row := range m.worktreeRows() {
		if row.header {
			continue
		}

		switch m.worktreeFate(row.item) {
		case fateArchived:
			deletion.agents = append(deletion.agents, row.item)
		case fateUnowned:
			deletion.unowned = append(deletion.unowned, row.item)
		case fateLive:
		}
	}

	return m.deleteWorktreesConfirm(deletion)
}

// worktreeDeletion is what one delete takes: the directories that go through their agent, and
// the ones the remover takes on its own.
type worktreeDeletion struct {
	agents  []worktreeItem
	unowned []worktreeItem
}

func (d worktreeDeletion) isEmpty() bool {
	return len(d.agents)+len(d.unowned) == 0
}

func (m model) deleteWorktreesConfirm(deletion worktreeDeletion) (tea.Model, tea.Cmd) {
	if deletion.isEmpty() {
		return m, nil
	}

	form, confirmed := deleteWorktreesForm(deletion, m.keepsBranch(deletion), formKeys())

	return m, m.openForm(formState{
		kind:   formWorktreesDelete,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.deleteWorktreesCmd(deletion)
		}),
	})
}

// keepsBranch says whether a single directory's branch would be kept, and nothing for more than
// one or for one not yet read.
func (m model) keepsBranch(deletion worktreeDeletion) *bool {
	if len(deletion.agents) != 1 || len(deletion.unowned) != 0 {
		return nil
	}

	read, ok := m.worktrees.inspected[deletion.agents[0].dir]
	if !ok || read.err != nil || read.baseRef == "" {
		return nil
	}

	return &read.keepsBranch
}

// deleteWorktreesCmd queues the deletion as one background job.
func (m *model) deleteWorktreesCmd(deletion worktreeDeletion) tea.Cmd {
	dirs := xslice.Map(slices.Concat(deletion.agents, deletion.unowned), func(item worktreeItem) string { return item.dir })
	subject := label.Count(len(dirs), "worktree", "worktrees")

	return m.queuedCmd(
		"Could not delete "+subject,
		message{
			kind:    messageInfo,
			summary: "Deleting " + subject,
			detail:  "A background job is taking the directories. The rows are out of the list now, and one it refuses comes back, with the reason in the log.",
		},
		func(a api) error { return a.deleteWorktrees(dirs) },
	)
}
