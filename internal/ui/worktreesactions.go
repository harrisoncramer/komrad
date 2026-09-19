package ui

import (
	"fmt"
	"slices"

	"weave/config"
	agentstore "weave/internal/store/agent"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
)

// deleteAgentsBatch is how many ids one delete_agents call takes.
const deleteAgentsBatch = 25

// removeWorktreesBatch is how many directories one remove_unowned_worktrees call takes.
const removeWorktreesBatch = 100

// worktreesActions is the worktrees screen as one table, walked in precedence order by the
// dispatch and named in reading order by a legend of the same ids.
func worktreesActions() actionSpec {
	return actionSpec{
		table:  worktreesTable(),
		legend: worktreesLegend(),
	}
}

func worktreesTable() []action {
	selecting := func(m model) bool { return m.worktrees.selected.isActive() }
	browsing := func(m model) bool { return !m.worktrees.selected.isActive() }

	return []action{
		{
			id:   actionJump,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Top, k.Nav.Bottom) },
			run:  model.jumpWorktrees,
		},
		{
			id:   actionSelectClear,
			keys: func(k config.Keybindings) []string { return k.Selection.Clear },
			when: selecting,
			run:  model.clearWorktreeSelection,
		},
		{
			id:   actionSelectToggle,
			keys: func(k config.Keybindings) []string { return k.Selection.Toggle },
			run:  model.toggleWorktreeSelection,
		},
		{
			id:   actionSelectRange,
			keys: func(k config.Keybindings) []string { return k.Selection.Range },
			run:  model.extendWorktreeSelection,
		},
		{
			id:   actionDeleteSelected,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Delete },
			when: selecting,
			run:  model.deleteChosenWorktrees,
		},
		{
			id:   actionQuit,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Quit },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m, m.hide() },
		},
		{
			id:   actionMove,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Up, k.Nav.Down) },
			run:  model.moveWorktrees,
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
			when: func(m model) bool { return !m.worktrees.selected.isActive() && m.filter.isStanding() },
			run:  model.jumpWorktreeMatch,
		},
		{
			id:   actionFilterClear,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Back },
			when: func(m model) bool { return !m.worktrees.selected.isActive() && m.filter.isStanding() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.clearSearch() },
		},
		{
			id:   actionWorktreesBack,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Back },
			when: browsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.returnToAgents() },
		},
		{
			id:   actionWorktreesEnter,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Enter },
			when: model.canWorktreeRowOpen,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.jumpToWorktreeAgent() },
		},
		{
			id:   actionWorktreesDelete,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Delete },
			when: browsing,
			run:  model.deleteHoveredWorktree,
		},
		{
			id:   actionWorktreesClear,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Clear },
			when: browsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.clearWorktreesConfirm() },
		},
	}
}

func worktreesLegend() []legendRow {
	selecting := func(m model) bool { return m.worktrees.selected.isActive() }

	return []legendRow{
		{
			of:    []actionID{actionSelectToggle},
			desc:  model.worktreeToggleDesc,
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
			desc:  model.worktreeClearDesc,
			show:  selecting,
			strip: true,
		},
		{
			of:   []actionID{actionDeleteSelected},
			desc: func(model) string { return "delete all" },
			show: selecting,
		},
		{
			of:    []actionID{actionWorktreesEnter},
			desc:  func(model) string { return "open the agent" },
			show:  model.canWorktreeRowOpen,
			strip: true,
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: model.worktreeToggleDesc,
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
			of:   []actionID{actionWorktreesBack},
			desc: func(model) string { return "back" },
		},
		{
			of:    []actionID{actionFilterClear},
			desc:  func(model) string { return "clear search" },
			strip: true,
		},
		{
			of:    []actionID{actionFilterMatch},
			desc:  func(model) string { return "next/prev match" },
			strip: true,
		},
	}
}

func (m model) doesWorktreesHoldARow() bool {
	if m.worktrees.selected.isActive() {
		return false
	}

	_, ok := rowAt(m.worktreeRows(), m.worktrees.cursor)

	return ok
}

// doesWorktreesHoldADirectory reports whether the cursor is on a directory rather than on a
// repository or an agent working in somebody else's.
func (m model) doesWorktreesHoldADirectory() bool {
	if m.worktrees.selected.isActive() {
		return false
	}

	row, ok := m.hoveredWorktree()

	return ok && row.occupant == nil
}

// canWorktreeRowOpen reports whether the hovered row has an agent the panel can land on.
func (m model) canWorktreeRowOpen() bool {
	if m.worktrees.selected.isActive() {
		return false
	}

	row, ok := m.hoveredWorktree()

	return ok && row.agentOn() != nil
}

func (m model) jumpWorktrees(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Bottom) {
		return m.jumpWorktreeCursor(len(m.worktreeRows()) - 1)
	}

	return m, nil
}

func (m model) moveWorktrees(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Down) {
		return m.moveWorktreeCursor(1)
	}

	return m.moveWorktreeCursor(-1)
}

func (m model) jumpWorktreeMatch(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Filter.PrevMatch) {
		return m.jumpMatch(-1)
	}

	return m.jumpMatch(1)
}

func (m model) clearWorktreeSelection(string) (tea.Model, tea.Cmd) {
	m.worktrees.selected = selection{}

	return m, nil
}

func (m model) toggleWorktreeSelection(string) (tea.Model, tea.Cmd) {
	m.worktrees.selected = m.worktrees.selected.toggle(worktreeRefs(m.worktreeRows()), m.worktrees.cursor)

	return m, nil
}

func (m model) extendWorktreeSelection(string) (tea.Model, tea.Cmd) {
	if !m.worktrees.selected.isActive() {
		return m.toggleWorktreeSelection("")
	}

	m.worktrees.selected = m.worktrees.selected.extend(worktreeRefs(m.worktreeRows()), m.worktrees.cursor)

	return m, nil
}

func (m model) worktreeToggleDesc() string {
	hovered, _ := rowAt(m.worktreeRows(), m.worktrees.cursor)

	switch {
	case hovered.header:
		return "select repository"
	case m.worktrees.selected.has(hovered.key()):
		return "deselect"
	}

	return "select"
}

func (m model) worktreeClearDesc() string {
	return fmt.Sprintf("clear %d selected", len(m.worktrees.selected.keys))
}

// worktreeDeleteDesc names what the delete key does to the hovered directory, which depends on
// who answers for it.
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

// hoveredWorktreeAgent is the agent the hovered row stands for, as the listing that holds it
// knows it.
func (m model) hoveredWorktreeAgent() (agentstore.Agent, bool) {
	row, ok := m.hoveredWorktree()
	if !ok {
		return agentstore.Agent{}, false
	}

	agent := row.agentOn()
	if agent == nil {
		return agentstore.Agent{}, false
	}

	if item, ok := m.liveAgent(agent.id); ok {
		return item.agent, true
	}

	return m.archivedAgent(agent.id)
}

// jumpToWorktreeAgent lands the agents list or the archive on the hovered row's agent.
func (m model) jumpToWorktreeAgent() (tea.Model, tea.Cmd) {
	row, ok := m.hoveredWorktree()
	if !ok || row.agentOn() == nil {
		return m, nil
	}

	id := row.agentOn().id
	if _, live := m.liveAgent(id); live {
		m.enterScreen(screenAgents)
		m.revealAgent(id)
		m.stageSelection()

		return m, nil
	}

	m.archive.focus = id
	if idx := rowIndexOfAgentID(m.archiveRows(), id); idx >= 0 {
		m.archive.cursor = idx
	}

	return m.enterArchive()
}

func (m model) deleteHoveredWorktree(string) (tea.Model, tea.Cmd) {
	row, ok := m.hoveredWorktree()
	if !ok || row.occupant != nil {
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
		if row.header || row.occupant != nil || !m.worktrees.selected.has(row.key()) {
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
		if row.header || row.occupant != nil {
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

// bytes is what the deletion's directories hold, and whether every one of them has been counted.
func (d worktreeDeletion) bytes() (int64, bool) {
	var total int64
	counted := true
	for _, item := range slices.Concat(d.agents, d.unowned) {
		if item.bytes == nil {
			counted = false

			continue
		}

		total += *item.bytes
	}

	return total, counted
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

// keepsBranch says whether a single directory's branch would be kept, read off its inspection,
// and nothing at all for more than one or for one not yet read.
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

// deleteWorktreesCmd runs the two paths a deletion takes, each in the batches its call allows.
func (m *model) deleteWorktreesCmd(deletion worktreeDeletion) tea.Cmd {
	ids := xslice.Map(deletion.agents, func(item worktreeItem) string { return item.agent.id })
	dirs := xslice.Map(deletion.unowned, func(item worktreeItem) string { return item.dir })

	return m.writeCmd(describeDeletion(deletion), func(a api) error {
		for batch := range slices.Chunk(ids, deleteAgentsBatch) {
			if err := a.deleteAgents(batch); err != nil {
				return err
			}
		}
		for batch := range slices.Chunk(dirs, removeWorktreesBatch) {
			removed, err := a.removeUnownedWorktrees(batch)
			if err != nil {
				return err
			}
			if refused := removed.GetRefused(); len(refused) > 0 {
				return fmt.Errorf("%s was left alone: %s", refused[0].GetDir(), refused[0].GetReason())
			}
		}

		return nil
	})
}

func describeDeletion(deletion worktreeDeletion) string {
	return fmt.Sprintf("Could not delete %s", countWord(len(deletion.agents)+len(deletion.unowned), "worktree", "worktrees"))
}
