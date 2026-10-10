package ui

import (
	"slices"

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
			keys: fixedBack,
			when: list.isBrowsing,
			nav:  func(m *model, _ string) tea.Cmd { return m.pickView() },
		},
	)
}

func (w *worktreesScreen) legend() []screenLegendRow {
	list := w.list()

	return slices.Concat(list.selectionLegend(), []screenLegendRow{
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

func (w *worktreesScreen) holdsARow(m *model) bool {
	if w.selected.isActive() {
		return false
	}

	_, ok := rowAt(w.rows(m), w.cursor)

	return ok
}

// worktreeFate is which of the four delete paths a directory takes.
type worktreeFate int

const (
	// fateUnowned is a directory nothing answers for, which the remover takes.
	fateUnowned worktreeFate = iota
	// fateArchived is a directory whose agent is put away, which the agent delete takes.
	fateArchived
	// fateLive is a directory whose agent is running, which is offered the archive.
	fateLive
	// fateTask is a directory only its task answers for, which the task delete takes.
	fateTask
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
	case fateTask:
		return "delete the task"
	}

	return ""
}

// deleteWorktree asks before a directory goes the way its fate decides.
func deleteWorktree(m *model, item worktreeItem) tea.Cmd {
	switch item.fate {
	case fateLive:
		if live, ok := m.listing.liveAgent(item.agent.id); ok {
			return confirmArchiveAgents(m, []panel.Agent{live.Agent})
		}
	case fateArchived:
		return confirmDeleteWorktrees(m, worktreeDeletion{agents: []worktreeItem{item}})
	case fateUnowned:
		return confirmDeleteWorktrees(m, worktreeDeletion{unowned: []worktreeItem{item}})
	case fateTask:
		if item.task != nil {
			return confirmDeleteTasks(m, []panel.Task{*item.task})
		}
	}

	return nil
}

// chosenDeletion is what deleting every chosen directory takes.
func (w *worktreesScreen) chosenDeletion(m *model) worktreeDeletion {
	deletion := worktreeDeletion{}
	for _, row := range w.rows(m) {
		if row.header || !w.selected.has(row.key()) {
			continue
		}

		switch row.item.fate {
		case fateLive:
			if item, ok := m.listing.liveAgent(row.item.agent.id); ok {
				deletion.live = append(deletion.live, item.Agent)
			}
			deletion.agents = append(deletion.agents, row.item)
		case fateArchived:
			deletion.agents = append(deletion.agents, row.item)
		case fateUnowned:
			deletion.unowned = append(deletion.unowned, row.item)
		case fateTask:
			if row.item.task != nil {
				deletion.tasks = append(deletion.tasks, *row.item.task)
			}
		}
	}
	deletion.children = unnamedChildren(m.listing.liveAgents(), deletion.live)

	return deletion
}

// clearDeletion is everything on the screen that can go without stopping an agent.
func (w *worktreesScreen) clearDeletion(m *model) worktreeDeletion {
	deletion := worktreeDeletion{}
	for _, row := range w.rows(m) {
		if row.header {
			continue
		}

		switch row.item.fate {
		case fateArchived:
			deletion.agents = append(deletion.agents, row.item)
		case fateUnowned:
			deletion.unowned = append(deletion.unowned, row.item)
		case fateLive, fateTask:
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
	// tasks are the chosen directories that go with their task, which the task delete takes.
	tasks []panel.Task
}

func (d worktreeDeletion) count() int {
	return len(d.agents) + len(d.unowned)
}

func (d worktreeDeletion) isEmpty() bool {
	return d.count() == 0
}

// chosen is how many chosen directories the deletion holds, task checkouts included.
func (d worktreeDeletion) chosen() int {
	return d.count() + len(d.tasks)
}

// confirmDeleteChosen asks before the chosen directories go, refusing a choice that mixes task
// checkouts with the rest.
func confirmDeleteChosen(m *model, deletion worktreeDeletion) tea.Cmd {
	switch {
	case len(deletion.tasks) == 0:
		return confirmDeleteWorktrees(m, deletion)
	case deletion.isEmpty():
		return confirmDeleteTasks(m, deletion.tasks)
	}

	m.reportBrief(
		"Could not delete "+label.Count(deletion.chosen(), "worktree", "worktrees"),
		"a task's worktree goes with its task, so choose those apart from the rest",
	)

	return nil
}

// confirmDeleteWorktrees asks before a deletion, saying where a lone branch would stand.
func confirmDeleteWorktrees(m *model, deletion worktreeDeletion) tea.Cmd {
	if deletion.isEmpty() {
		return nil
	}

	form, confirmed := deleteWorktreesForm(deletion, keepsBranch(m, deletion), fixedKeys())

	return m.openForm(formState{
		kind:   formWorktreesDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			pending := m.unstageAhead(deletion.live)

			return pending.attach(deleteWorktreesCmd(m, deletion, pending))
		}),
	})
}

// keepsBranch says whether a single directory's branch would be kept, and nothing for more than
// one or for one not yet read.
func keepsBranch(m *model, deletion worktreeDeletion) *bool {
	if len(deletion.agents) != 1 || len(deletion.unowned) != 0 {
		return nil
	}

	read, ok := m.inspections[deletion.agents[0].dir]
	if !ok || read.err != nil || read.baseRef == "" {
		return nil
	}

	return &read.keepsBranch
}

// deleteWorktreesCmd queues the deletion as one background job.
func deleteWorktreesCmd(m *model, deletion worktreeDeletion, pending *unstaged) tea.Cmd {
	dirs := xslice.Map(slices.Concat(deletion.agents, deletion.unowned), func(item worktreeItem) string { return item.dir })
	subject := label.Count(len(dirs), "worktree", "worktrees")

	deleting := m.awaitingWriteCmd(
		"deleting "+subject,
		"Could not delete "+subject,
		&awaits{gone: rowsIn(listWorktrees, dirs...)},
		func(a api) error {
			if len(deletion.live) > 0 {
				if err := pending.noteArchive(a.archiveAgents(panel.IDs(deletion.live))); err != nil {
					return err
				}
			}

			return a.deleteWorktrees(dirs)
		},
	)

	return announced(deleting, message{
		kind:    messageInfo,
		summary: "Deleting " + subject,
		detail:  "A background job is taking the directories. The rows are out of the list now, and one it refuses comes back, with the reason in the log.",
	})
}

func (w *worktreesScreen) buildItemMenu(m *model) (submenu, bool) {
	if w.list().isSelecting(m) {
		deletion := w.chosenDeletion(m)
		if deletion.chosen() == 0 {
			return submenu{}, false
		}

		return withNested(rowMenu(label.Count(deletion.chosen(), "worktree", "worktrees")), "archive", m.keys.Menu.Archive, liveArchiveMenu(openEntry("delete all", m.keys.ArchiveMenu.Delete, func(m *model) tea.Cmd {
			return confirmDeleteChosen(m, deletion)
		}))), true
	}

	row, ok := rowAt(w.rows(m), w.cursor)
	if !ok {
		return submenu{}, false
	}

	var entries []submenuEntry
	if !row.header {
		keys := m.keys.ArchiveMenu.Delete
		if row.item.fate == fateLive {
			keys = m.keys.ArchiveMenu.Archive
		}

		entries = append(entries, openEntry(row.item.fate.deleteDesc(), keys, func(m *model) tea.Cmd {
			return deleteWorktree(m, row.item)
		}))
	}

	clearable := w.clearDeletion(m)
	clearAll := openEntry("clear what can go", m.keys.ArchiveMenu.Clear, func(m *model) tea.Cmd {
		return confirmDeleteWorktrees(m, clearable)
	})
	clearAll.isOff = clearable.isEmpty()

	menu := withNested(rowMenu(row.name()), "archive", m.keys.Menu.Archive, liveArchiveMenu(append(entries, clearAll)...))
	if row.header || row.item.agent == nil {
		return menu, true
	}

	return withNested(menu, "jump", m.keys.Menu.Jump, jumpMenu(m, screenWorktrees, row.item.agent.id)), true
}
