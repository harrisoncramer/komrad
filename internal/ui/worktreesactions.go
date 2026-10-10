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

// worktreeFate is which of the delete paths a directory takes.
type worktreeFate int

const (
	// fateUnowned is a directory no task answers for, which the remover takes.
	fateUnowned worktreeFate = iota
	// fateLive is a directory whose task is open, which is offered the task's archive.
	fateLive
	// fateArchived is a directory whose task is put away, which the task delete takes.
	fateArchived
	// fateDeleting is a directory whose task a delete has claimed, which nothing more is asked of.
	fateDeleting
)

// deleteDesc names what the delete entry does to a directory of this fate, empty when it offers none.
func (f worktreeFate) deleteDesc() string {
	switch f {
	case fateLive:
		return "archive the task"
	case fateArchived:
		return "delete the task"
	case fateUnowned:
		return "remove"
	case fateDeleting:
	}

	return ""
}

// deleteWorktree asks before a directory goes the way its fate decides.
func deleteWorktree(m *model, item worktreeItem) tea.Cmd {
	switch item.fate {
	case fateLive:
		return m.archiveTasks([]panel.Task{*item.task})
	case fateArchived:
		return confirmDeleteTasks(m, []panel.Task{*item.task})
	case fateUnowned:
		return confirmDeleteWorktrees(m, worktreeDeletion{unowned: []worktreeItem{item}})
	case fateDeleting:
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
		case fateLive, fateArchived:
			deletion.tasks = append(deletion.tasks, *row.item.task)
		case fateUnowned:
			deletion.unowned = append(deletion.unowned, row.item)
		case fateDeleting:
		}
	}

	return deletion
}

// clearDeletion is every directory on the screen no task answers for.
func (w *worktreesScreen) clearDeletion(m *model) worktreeDeletion {
	deletion := worktreeDeletion{}
	for _, row := range w.rows(m) {
		if !row.header && row.item.fate == fateUnowned {
			deletion.unowned = append(deletion.unowned, row.item)
		}
	}

	return deletion
}

// worktreeDeletion is what one delete takes: the directories the remover takes on its own, and
// the tasks whose delete takes theirs.
type worktreeDeletion struct {
	unowned []worktreeItem
	tasks   []panel.Task
}

func (d worktreeDeletion) count() int {
	return len(d.unowned)
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

// confirmDeleteWorktrees asks before a deletion.
func confirmDeleteWorktrees(m *model, deletion worktreeDeletion) tea.Cmd {
	if deletion.isEmpty() {
		return nil
	}

	form, confirmed := deleteWorktreesForm(deletion, fixedKeys())

	return m.openForm(formState{
		kind:   formWorktreesDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			return deleteWorktreesCmd(m, deletion)
		}),
	})
}

// deleteWorktreesCmd queues the deletion as one background job.
func deleteWorktreesCmd(m *model, deletion worktreeDeletion) tea.Cmd {
	dirs := xslice.Map(deletion.unowned, func(item worktreeItem) string { return item.dir })
	subject := label.Count(len(dirs), "worktree", "worktrees")

	deleting := m.awaitingWriteCmd(
		"deleting "+subject,
		"Could not delete "+subject,
		&awaits{gone: rowsIn(listWorktrees, dirs...)},
		func(a api) error {
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
		if entry, ok := worktreeDeleteEntry(m, row.item); ok {
			entries = append(entries, entry)
		}
	}

	clearable := w.clearDeletion(m)
	clearAll := openEntry("clear what can go", m.keys.ArchiveMenu.Clear, func(m *model) tea.Cmd {
		return confirmDeleteWorktrees(m, clearable)
	})
	clearAll.isOff = clearable.isEmpty()

	menu := withNested(rowMenu(row.name()), "archive", m.keys.Menu.Archive, liveArchiveMenu(append(entries, clearAll)...))
	if row.header {
		return menu, true
	}

	lead, ok := row.item.findLead()
	if !ok {
		return menu, true
	}

	return withNested(menu, "jump", m.keys.Menu.Jump, jumpMenu(m, screenWorktrees, lead.id)), true
}

// worktreeDeleteEntry is the entry that sends a directory the way its fate decides, false when
// the fate offers none.
func worktreeDeleteEntry(m *model, item worktreeItem) (submenuEntry, bool) {
	desc := item.fate.deleteDesc()
	run := func(m *model) tea.Cmd { return deleteWorktree(m, item) }
	switch item.fate {
	case fateLive:
		return writeEntry(desc, m.keys.ArchiveMenu.Archive, run), true
	case fateArchived, fateUnowned:
		return openEntry(desc, m.keys.ArchiveMenu.Delete, run), true
	case fateDeleting:
	}

	return submenuEntry{}, false
}
