package ui

import (
	"slices"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// actions is the worktrees screen as one table and the legend that names it.
func (w *worktreesScreen) actions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table:  w.table(),
		Legend: w.legend(),
	}
}

func (w *worktreesScreen) table() []keys.Action[*model] {
	list := w.list()

	return append(list.actions(),
		keys.Action[*model]{
			ID:   actionWorktreesBack,
			Keys: keys.FixedBack,
			When: list.isBrowsing,
			Nav:  func(m *model, _ string) tea.Cmd { return m.pickView() },
		},
	)
}

func (w *worktreesScreen) legend() []keys.LegendRow[*model] {
	list := w.list()

	return slices.Concat(list.selectionLegend(), []keys.LegendRow[*model]{
		{
			Of:   []keys.ID{keys.SelectToggle},
			Desc: list.toggleDesc,
			Show: w.holdsARow,
		},
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf,
		},
	}, keys.NavLegend[*model](), []keys.LegendRow[*model]{
		{
			Of:   []keys.ID{actionWorktreesBack},
			Desc: func(*model) string { return "back" },
		},
		{
			Of:    []keys.ID{keys.FilterClear},
			Desc:  filterClearDescOf,
			Strip: true,
		},
		{
			Of:    []keys.ID{keys.FilterMatch},
			Desc:  func(*model) string { return "next/prev match" },
			Strip: true,
		},
	})
}

func (w *worktreesScreen) holdsARow(m *model) bool {
	if w.Selected.IsActive() {
		return false
	}

	_, ok := list.At(w.rows(m), w.Row)

	return ok
}

// deleteDesc names what the delete entry does to a directory of the fate, empty when it offers none.
func deleteDesc(fate panel.WorktreeFate) string {
	switch fate {
	case panel.FateLive:
		return "archive the task"
	case panel.FateArchived:
		return "delete the task"
	case panel.FateUnowned:
		return "remove"
	case panel.FateDeleting:
	}

	return ""
}

// deleteWorktree asks before a directory goes the way its fate decides.
func deleteWorktree(m *model, item panel.Worktree) tea.Cmd {
	switch item.Fate {
	case panel.FateLive:
		return m.archiveTasks([]panel.Task{*item.Task})
	case panel.FateArchived:
		return confirmDeleteTasks(m, []panel.Task{*item.Task})
	case panel.FateUnowned:
		return confirmDeleteWorktrees(m, worktreeDeletion{unowned: []panel.Worktree{item}})
	case panel.FateDeleting:
	}

	return nil
}

// chosenDeletion is what deleting every chosen directory takes.
func (w *worktreesScreen) chosenDeletion(m *model) worktreeDeletion {
	deletion := worktreeDeletion{}
	for _, row := range w.rows(m) {
		if row.header || !w.Selected.Has(row.Key()) {
			continue
		}

		switch row.item.Fate {
		case panel.FateLive, panel.FateArchived:
			deletion.tasks = append(deletion.tasks, *row.item.Task)
		case panel.FateUnowned:
			deletion.unowned = append(deletion.unowned, row.item)
		case panel.FateDeleting:
		}
	}

	return deletion
}

// clearDeletion is every directory on the screen no task answers for.
func (w *worktreesScreen) clearDeletion(m *model) worktreeDeletion {
	deletion := worktreeDeletion{}
	for _, row := range w.rows(m) {
		if !row.header && row.item.Fate == panel.FateUnowned {
			deletion.unowned = append(deletion.unowned, row.item)
		}
	}

	return deletion
}

// worktreeDeletion is what one delete takes: the directories the remover takes on its own, and
// the tasks whose delete takes theirs.
type worktreeDeletion struct {
	unowned []panel.Worktree
	tasks   []panel.Task
}

func (d worktreeDeletion) count() int {
	return len(d.unowned)
}

func (d worktreeDeletion) isEmpty() bool {
	return d.count() == 0
}

// dirs is the directories the remover takes.
func (d worktreeDeletion) dirs() []string {
	return xslice.Map(d.unowned, func(item panel.Worktree) string { return item.Dir })
}

// chosen is how many chosen directories the deletion holds, task checkouts included.
func (d worktreeDeletion) chosen() int {
	return d.count() + len(d.tasks)
}

// confirmDeleteChosen asks before the chosen directories go, with the confirm that fits the choice.
func confirmDeleteChosen(m *model, deletion worktreeDeletion) tea.Cmd {
	switch {
	case len(deletion.tasks) == 0:
		return confirmDeleteWorktrees(m, deletion)
	case deletion.isEmpty():
		return confirmDeleteTasks(m, deletion.tasks)
	default:
		return confirmDeleteMixed(m, deletion)
	}
}

// confirmDeleteMixed asks once before task checkouts go with their tasks and the other directories
// through the remover.
func confirmDeleteMixed(m *model, deletion worktreeDeletion) tea.Cmd {
	confirm := deleteMixedConfirm(deletion, len(m.listing.AgentsOnTasks(deletion.tasks)))

	return confirm.open(m, func() screen.Request {
		return screen.Batch{m.deleteTasks(deletion.tasks, confirm.values.deletesRecords()), deleteWorktrees(m, deletion)}
	})
}

// confirmDeleteWorktrees asks before a deletion.
func confirmDeleteWorktrees(m *model, deletion worktreeDeletion) tea.Cmd {
	if deletion.isEmpty() {
		return nil
	}

	return deleteWorktreesConfirm(deletion).open(m, func() screen.Request {
		return deleteWorktrees(m, deletion)
	})
}

// deleteWorktrees queues the deletion as one background job.
func deleteWorktrees(m *model, deletion worktreeDeletion) screen.Write {
	dirs := deletion.dirs()
	subject := label.Count(len(dirs), "worktree", "worktrees")

	clients := m.api

	return screen.Write{
		Doing:   "deleting " + subject,
		Summary: "Could not delete " + subject,
		Send: func() screen.Landed {
			return screen.Landed{
				Err:    clients.deleteWorktrees(dirs),
				Awaits: screen.Awaits{Gone: screen.RowsIn(screen.ListWorktrees, dirs...)},
				Notice: &screen.Notice{
					Summary: "Deleting " + subject,
					Detail:  "A background job deletes the directories. Komrad drops their rows from the list now. If the job refuses one, its row comes back and the log says why.",
				},
			}
		},
	}
}

func (w *worktreesScreen) buildItemMenu(m *model) (menu.Menu, bool) {
	if w.list().isSelecting(m) {
		deletion := w.chosenDeletion(m)
		if deletion.chosen() == 0 {
			return menu.Menu{}, false
		}

		return menu.WithNested(rowMenu(label.Count(deletion.chosen(), "worktree", "worktrees")), "archive", m.keys.Menu.Archive, liveArchiveMenu(menu.Leaving("delete all", m.keys.ArchiveMenu.Delete, func() tea.Cmd {
			return confirmDeleteChosen(m, deletion)
		}))), true
	}

	row, ok := list.At(w.rows(m), w.Row)
	if !ok {
		return menu.Menu{}, false
	}

	if row.header {
		return menu.Menu{}, false
	}

	band := rowMenu(row.name())
	if entry, ok := worktreeDeleteEntry(m, row.item); ok {
		band = menu.WithNested(band, "archive", m.keys.Menu.Archive, liveArchiveMenu(entry))
	}

	occupant, ok := row.item.FirstOccupant()
	if !ok {
		return band, len(band.Entries) > 0
	}

	to := screen.Target{AgentID: occupant.ID}
	if row.item.Task != nil {
		to.TaskID = row.item.Task.ID
	}

	return menu.WithNested(band, "jump", m.keys.Menu.Jump, jumpMenu(m, screen.Worktrees, to)), true
}

// buildScreenMenu builds the worktrees screen's own band, for clearing every directory that can go.
func (w *worktreesScreen) buildScreenMenu(m *model) (menu.Menu, bool) {
	clearable := w.clearDeletion(m)
	clearAll := menu.Leaving("delete unused worktrees", m.keys.ArchiveMenu.Clear, func() tea.Cmd {
		return confirmDeleteWorktrees(m, clearable)
	})
	clearAll.Off = clearable.isEmpty()

	return menu.Menu{
		Title:   "worktrees",
		Entries: []menu.Entry{clearAll},
	}, true
}

// worktreeDeleteEntry is the entry that sends a directory the way its fate decides, false when
// the fate offers none.
func worktreeDeleteEntry(m *model, item panel.Worktree) (menu.Entry, bool) {
	desc := deleteDesc(item.Fate)
	run := func() tea.Cmd { return deleteWorktree(m, item) }
	switch item.Fate {
	case panel.FateLive:
		return menu.Holding(desc, m.keys.ArchiveMenu.Archive, run), true
	case panel.FateArchived, panel.FateUnowned:
		return menu.Leaving(desc, m.keys.ArchiveMenu.Delete, run), true
	case panel.FateDeleting:
	}

	return menu.Entry{}, false
}
