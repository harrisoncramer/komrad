package ui

import (
	"slices"

	"weave/config"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
)

// actions is the worktrees screen as one table and the legend that names it.
func (w *worktreesScreen) actions() screenActionSpec {
	return screenActionSpec{
		table:  w.table(),
		legend: w.legend(),
	}
}

func (w *worktreesScreen) table() []screenAction {
	list := w.list()

	return append(list.actions(),
		screenAction{
			id:   actionDeleteSelected,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Delete },
			when: list.isSelecting,
			run:  w.deleteChosen,
		},
		screenAction{
			id:   actionWorktreesBack,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Back },
			when: list.isBrowsing,
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.enter(screenAgents, screenTarget{}) },
		},
		screenAction{
			id:   actionWorktreesJumpToAgent,
			keys: func(k config.Keybindings) []string { return k.Worktrees.JumpToAgent },
			when: w.canRowOpen,
			run:  func(h host, s panelShared, _ string) tea.Cmd { return w.jumpToAgent(h, s) },
		},
		screenAction{
			id:   actionWorktreesDelete,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Delete },
			when: list.isBrowsing,
			run:  w.deleteHovered,
		},
		screenAction{
			id:   actionWorktreesClear,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Clear },
			when: list.isBrowsing,
			run:  func(h host, s panelShared, _ string) tea.Cmd { return w.clearConfirm(h, s) },
		},
	)
}

func (w *worktreesScreen) legend() []screenLegendRow {
	list := w.list()

	return slices.Concat(list.selectionLegend(), []screenLegendRow{
		{
			of:   []actionID{actionDeleteSelected},
			desc: func(panelShared) string { return "delete all" },
			show: list.isSelecting,
		},
		{
			of:    []actionID{actionWorktreesJumpToAgent},
			desc:  func(panelShared) string { return "jump to agent" },
			show:  w.canRowOpen,
			strip: true,
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: list.toggleDesc,
			show: w.holdsARow,
		},
		{
			of:    []actionID{actionWorktreesDelete},
			desc:  w.deleteDesc,
			show:  w.holdsADirectory,
			strip: true,
		},
		{
			of:   []actionID{actionWorktreesClear},
			desc: func(panelShared) string { return "clear what can go" },
			show: w.holdsARow,
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: filterDescOf,
		},
	}, screenNavLegend(), []screenLegendRow{
		{
			of:   []actionID{actionWorktreesBack},
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

func (w *worktreesScreen) holdsARow(s panelShared) bool {
	if w.selected.isActive() {
		return false
	}

	_, ok := rowAt(w.rows(s), w.cursor)

	return ok
}

// holdsADirectory reports whether the cursor is on a directory rather than on a repository.
func (w *worktreesScreen) holdsADirectory(s panelShared) bool {
	if w.selected.isActive() {
		return false
	}

	_, ok := w.hovered(s)

	return ok
}

// canRowOpen reports whether the hovered row has an agent the panel can land on.
func (w *worktreesScreen) canRowOpen(s panelShared) bool {
	if w.selected.isActive() {
		return false
	}

	row, ok := w.hovered(s)

	return ok && row.item.agent != nil
}

// deleteDesc names what the delete key does to the hovered directory.
func (w *worktreesScreen) deleteDesc(s panelShared) string {
	row, ok := w.hovered(s)
	if !ok {
		return "delete"
	}

	switch row.item.fate {
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

// jumpToAgent lands the agents list or the archive on the hovered row's agent.
func (w *worktreesScreen) jumpToAgent(h host, s panelShared) tea.Cmd {
	row, ok := w.hovered(s)
	if !ok || row.item.agent == nil {
		return nil
	}

	target := screenTarget{agentID: row.item.agent.id}
	if _, live := s.listing.liveAgent(target.agentID); live {
		return h.enter(screenAgents, target)
	}

	return h.enter(screenArchive, target)
}

func (w *worktreesScreen) deleteHovered(h host, s panelShared, _ string) tea.Cmd {
	row, ok := w.hovered(s)
	if !ok {
		return nil
	}

	switch row.item.fate {
	case fateLive:
		if item, ok := s.listing.liveAgent(row.item.agent.id); ok {
			return confirmArchiveAgents(h, s.listing.liveAgents(), []panel.Agent{item.agent})
		}
	case fateArchived:
		return confirmDeleteWorktrees(h, s, worktreeDeletion{agents: []worktreeItem{row.item}})
	case fateUnowned:
		return confirmDeleteWorktrees(h, s, worktreeDeletion{unowned: []worktreeItem{row.item}})
	}

	return nil
}

func (w *worktreesScreen) deleteChosen(h host, s panelShared, _ string) tea.Cmd {
	deletion := worktreeDeletion{}
	for _, row := range w.rows(s) {
		if row.header || !w.selected.has(row.key()) {
			continue
		}

		switch row.item.fate {
		case fateLive:
			h.reportBrief("A live agent is in the selection", "Archive "+row.name()+" from the agents list first; nothing bulk stops a running agent.")

			return nil
		case fateArchived:
			deletion.agents = append(deletion.agents, row.item)
		case fateUnowned:
			deletion.unowned = append(deletion.unowned, row.item)
		}
	}

	return confirmDeleteWorktrees(h, s, deletion)
}

// clearConfirm offers everything on the screen that can go without stopping an agent.
func (w *worktreesScreen) clearConfirm(h host, s panelShared) tea.Cmd {
	deletion := worktreeDeletion{}
	for _, row := range w.rows(s) {
		if row.header {
			continue
		}

		switch row.item.fate {
		case fateArchived:
			deletion.agents = append(deletion.agents, row.item)
		case fateUnowned:
			deletion.unowned = append(deletion.unowned, row.item)
		case fateLive:
		}
	}

	return confirmDeleteWorktrees(h, s, deletion)
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

// confirmDeleteWorktrees asks before a deletion, saying where a lone branch would stand.
func confirmDeleteWorktrees(h host, s panelShared, deletion worktreeDeletion) tea.Cmd {
	if deletion.isEmpty() {
		return nil
	}

	form, confirmed := deleteWorktreesForm(deletion, keepsBranch(s, deletion), formKeys())

	return h.openForm(formState{
		kind:   formWorktreesDelete,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, deleteWorktreesCmd(&m, deletion)
		}),
	})
}

// keepsBranch says whether a single directory's branch would be kept, and nothing for more than
// one or for one not yet read.
func keepsBranch(s panelShared, deletion worktreeDeletion) *bool {
	if len(deletion.agents) != 1 || len(deletion.unowned) != 0 {
		return nil
	}

	read, ok := s.inspections[deletion.agents[0].dir]
	if !ok || read.err != nil || read.baseRef == "" {
		return nil
	}

	return &read.keepsBranch
}

// deleteWorktreesCmd queues the deletion as one background job.
func deleteWorktreesCmd(h host, deletion worktreeDeletion) tea.Cmd {
	dirs := xslice.Map(slices.Concat(deletion.agents, deletion.unowned), func(item worktreeItem) string { return item.dir })
	subject := label.Count(len(dirs), "worktree", "worktrees")

	return h.queuedCmd(
		"Could not delete "+subject,
		message{
			kind:    messageInfo,
			summary: "Deleting " + subject,
			detail:  "A background job is taking the directories. The rows are out of the list now, and one it refuses comes back, with the reason in the log.",
		},
		func(a api) error { return a.deleteWorktrees(dirs) },
	)
}
