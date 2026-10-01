package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
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
			id:   actionWorktreesBack,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Back },
			when: list.isBrowsing,
			nav:  func(h navHost, _ panelShared, _ string) tea.Cmd { return h.pickView() },
		},
		screenAction{
			id:   actionWorktreesJumpToAgent,
			keys: func(k config.Keybindings) []string { return k.Worktrees.JumpToAgent },
			when: w.canRowOpen,
			nav:  func(h navHost, s panelShared, _ string) tea.Cmd { return w.jumpToAgent(h, s) },
		},
	)
}

func (w *worktreesScreen) legend() []screenLegendRow {
	list := w.list()

	return slices.Concat(list.selectionLegend(), []screenLegendRow{
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

// canRowOpen reports whether the hovered row has an agent the panel can land on.
func (w *worktreesScreen) canRowOpen(s panelShared) bool {
	if w.selected.isActive() {
		return false
	}

	row, ok := w.hovered(s)

	return ok && row.item.agent != nil
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

// deleteDesc names what the delete entry does to a directory of this fate.
func (f worktreeFate) deleteDesc() string {
	switch f {
	case fateLive:
		return "archive the agent"
	case fateArchived:
		return "delete the agent"
	case fateUnowned:
		return "remove"
	}

	return ""
}

// jumpToAgent lands the agents list or the archive on the hovered row's agent.
func (w *worktreesScreen) jumpToAgent(h navHost, s panelShared) tea.Cmd {
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

// deleteWorktree asks before a directory goes the way its fate decides.
func deleteWorktree(h host, s panelShared, item worktreeItem) tea.Cmd {
	switch item.fate {
	case fateLive:
		if live, ok := s.listing.liveAgent(item.agent.id); ok {
			return confirmArchiveAgents(h, s.listing.liveAgents(), []panel.Agent{live.Agent})
		}
	case fateArchived:
		return confirmDeleteWorktrees(h, s, worktreeDeletion{agents: []worktreeItem{item}})
	case fateUnowned:
		return confirmDeleteWorktrees(h, s, worktreeDeletion{unowned: []worktreeItem{item}})
	}

	return nil
}

// chosenDeletion is what deleting every chosen directory takes.
func (w *worktreesScreen) chosenDeletion(s panelShared) worktreeDeletion {
	deletion := worktreeDeletion{}
	for _, row := range w.rows(s) {
		if row.header || !w.selected.has(row.key()) {
			continue
		}

		switch row.item.fate {
		case fateLive:
			if item, ok := s.listing.liveAgent(row.item.agent.id); ok {
				deletion.live = append(deletion.live, item.Agent)
			}
			deletion.agents = append(deletion.agents, row.item)
		case fateArchived:
			deletion.agents = append(deletion.agents, row.item)
		case fateUnowned:
			deletion.unowned = append(deletion.unowned, row.item)
		}
	}
	deletion.children = unnamedChildren(s.listing.liveAgents(), deletion.live)

	return deletion
}

// clearDeletion is everything on the screen that can go without stopping an agent.
func (w *worktreesScreen) clearDeletion(s panelShared) worktreeDeletion {
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

	return deletion
}

// worktreeDeletion is what one delete takes: the directories that go through their agent, and
// the ones the remover takes on its own.
type worktreeDeletion struct {
	agents  []worktreeItem
	unowned []worktreeItem
	// live are the agents still on the agents list, which are archived before the delete.
	live []panel.Agent
	// children is how many agents not named in live go into the archive with them.
	children int
}

func (d worktreeDeletion) count() int {
	return len(d.agents) + len(d.unowned)
}

func (d worktreeDeletion) isEmpty() bool {
	return d.count() == 0
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
		submit: holdSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			m.releasePanes(deletion.live)

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
		"deleting "+subject,
		"Could not delete "+subject,
		message{
			kind:    messageInfo,
			summary: "Deleting " + subject,
			detail:  "A background job is taking the directories. The rows are out of the list now, and one it refuses comes back, with the reason in the log.",
		},
		func(a api) error {
			if len(deletion.live) > 0 {
				if err := a.archiveAgents(panel.IDs(deletion.live)); err != nil {
					return err
				}
			}

			return a.deleteWorktrees(dirs)
		},
	)
}

func (w *worktreesScreen) itemMenu(s panelShared) (submenu, bool) {
	if w.list().isSelecting(s) {
		deletion := w.chosenDeletion(s)
		if deletion.isEmpty() {
			return submenu{}, false
		}

		return withArchive(rowMenu(label.Count(deletion.count(), "worktree", "worktrees")), s, liveArchiveMenu(openEntry("delete all", s.keys.ArchiveMenu.Delete, func(h host, s panelShared) tea.Cmd {
			return confirmDeleteWorktrees(h, s, deletion)
		}))), true
	}

	row, ok := rowAt(w.rows(s), w.cursor)
	if !ok {
		return submenu{}, false
	}

	var entries []submenuEntry
	if !row.header {
		keys := s.keys.ArchiveMenu.Delete
		if row.item.fate == fateLive {
			keys = s.keys.ArchiveMenu.Archive
		}

		entries = append(entries, openEntry(row.item.fate.deleteDesc(), keys, func(h host, s panelShared) tea.Cmd {
			return deleteWorktree(h, s, row.item)
		}))
	}

	clearable := w.clearDeletion(s)
	clearAll := openEntry("clear what can go", s.keys.ArchiveMenu.Clear, func(h host, s panelShared) tea.Cmd {
		return confirmDeleteWorktrees(h, s, clearable)
	})
	clearAll.isOff = clearable.isEmpty()

	return withArchive(rowMenu(row.name()), s, liveArchiveMenu(append(entries, clearAll)...)), true
}
