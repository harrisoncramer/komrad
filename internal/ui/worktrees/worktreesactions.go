package worktrees

import (
	"slices"

	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
)

const actionBack keys.ID = keys.Own + iota

// actions is the worktrees screen as one table and the legend that names it.
func (w *Screen) actions(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table:  w.table(in),
		Legend: w.legend(in),
	}
}

func (w *Screen) table(in screen.Inputs) []keys.Action[*Screen] {
	list := w.list()

	return append(keys.ListActions[*Screen](list, in),
		keys.Action[*Screen]{
			ID:   actionBack,
			Keys: keys.FixedBack,
			When: func(*Screen) bool { return list.IsBrowsing() },
			Nav:  func(*Screen, string) screen.Request { return screen.FocusViewSwitcher{} },
		},
	)
}

func (w *Screen) legend(in screen.Inputs) []keys.LegendRow[*Screen] {
	list := w.list()

	return slices.Concat(keys.SelectionLegend[*Screen](list, in), []keys.LegendRow[*Screen]{
		{
			ActionIDs: []keys.ID{keys.SelectToggle},
			Desc:      func(*Screen) string { return list.ToggleDesc(in) },
			Show:      func(*Screen) bool { return w.holdsARow(in) },
		},
		{
			ActionIDs: []keys.ID{keys.FilterOpen},
			Desc:      keys.FilterDescOf[*Screen](in),
		},
	}, keys.NavLegend[*Screen](), []keys.LegendRow[*Screen]{
		{
			ActionIDs: []keys.ID{actionBack},
			Desc:      func(*Screen) string { return "back" },
		},
		{
			ActionIDs: []keys.ID{keys.FilterClear},
			Desc:      keys.FilterClearDescOf[*Screen](in),
			Strip:     true,
		},
		{
			ActionIDs: []keys.ID{keys.FilterMatch},
			Desc:      func(*Screen) string { return "next/prev match" },
			Strip:     true,
		},
	})
}

func (w *Screen) holdsARow(in screen.Inputs) bool {
	if w.Selected.IsActive() {
		return false
	}

	_, ok := list.At(w.rows(in), w.Row)

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
func (w *Screen) deleteWorktree(in screen.Inputs, item panel.Worktree) screen.Request {
	switch item.Fate {
	case panel.FateLive:
		tasks := []panel.Task{*item.Task}

		return w.deps.Clients.ArchiveTasksWrite(tasks, in.Listing.AgentsOnTasks(tasks))
	case panel.FateArchived:
		return w.deps.Clients.ConfirmDeleteTasks(in, []panel.Task{*item.Task})
	case panel.FateUnowned:
		return w.confirmDeleteWorktrees(in, deletion{unowned: []panel.Worktree{item}})
	case panel.FateDeleting:
	}

	return nil
}

// chosenDeletion is what deleting every chosen directory takes.
func (w *Screen) chosenDeletion(in screen.Inputs) deletion {
	deletion := deletion{}
	for _, row := range w.rows(in) {
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
func (w *Screen) clearDeletion(in screen.Inputs) deletion {
	deletion := deletion{}
	for _, row := range w.rows(in) {
		if !row.header && row.item.Fate == panel.FateUnowned {
			deletion.unowned = append(deletion.unowned, row.item)
		}
	}

	return deletion
}

// deletion is what one delete takes: the directories the remover takes on its own, and
// the tasks whose delete takes theirs.
type deletion struct {
	unowned []panel.Worktree
	tasks   []panel.Task
}

func (d deletion) count() int {
	return len(d.unowned)
}

func (d deletion) isEmpty() bool {
	return d.count() == 0
}

// dirs is the directories the remover takes.
func (d deletion) dirs() []string {
	return xslice.Map(d.unowned, func(item panel.Worktree) string { return item.Dir })
}

// chosen is how many chosen directories the deletion holds, task checkouts included.
func (d deletion) chosen() int {
	return d.count() + len(d.tasks)
}

// confirmDeleteChosen asks before the chosen directories go, with the confirm that fits the choice.
func (w *Screen) confirmDeleteChosen(in screen.Inputs, deletion deletion) screen.Request {
	switch {
	case len(deletion.tasks) == 0:
		return w.confirmDeleteWorktrees(in, deletion)
	case deletion.isEmpty():
		return w.deps.Clients.ConfirmDeleteTasks(in, deletion.tasks)
	default:
		return w.confirmDeleteMixed(in, deletion)
	}
}

// confirmDeleteMixed asks once before task checkouts go with their tasks and the other directories
// through the remover.
func (w *Screen) confirmDeleteMixed(in screen.Inputs, deletion deletion) screen.Request {
	confirm := deleteMixedConfirm(deletion, len(in.Listing.AgentsOnTasks(deletion.tasks)))

	return confirm.Open(in, func(submitted form.Inputs) screen.Request {
		return screen.Batch{w.deps.Clients.DeleteTasksWrite(deletion.tasks, submitted.Listing.AgentsOnTasks(deletion.tasks), confirm.Values.DeletesRecords()), w.deleteWorktreesWrite(deletion)}
	})
}

// confirmDeleteWorktrees asks before a deletion.
func (w *Screen) confirmDeleteWorktrees(in screen.Inputs, deletion deletion) screen.Request {
	if deletion.isEmpty() {
		return nil
	}

	return deleteWorktreesConfirm(deletion).Open(in, func(form.Inputs) screen.Request {
		return w.deleteWorktreesWrite(deletion)
	})
}

// deleteWorktreesWrite is the write that queues the deletion as one background job.
func (w *Screen) deleteWorktreesWrite(deletion deletion) screen.Write {
	dirs := deletion.dirs()
	subject := label.Count(len(dirs), "worktree", "worktrees")

	clients := w.deps.Clients

	return screen.Write{
		Doing:          "deleting " + subject,
		FailureSummary: "Could not delete " + subject,
		Send: func() screen.WriteResult {
			return screen.WriteResult{
				Err:     clients.DeleteWorktrees(dirs),
				WaitFor: screen.WaitFor{RowsGone: screen.RowsIn(screen.ListWorktrees, dirs...)},
				SuccessNotice: &screen.Notice{
					Summary: "Deleting " + subject,
					Detail:  "A background job deletes the directories. Komrad drops their rows from the list now. If the job refuses one, its row comes back and the log says why.",
				},
			}
		},
	}
}

// BuildItemMenu builds the band on the hovered directory, or on every chosen one while any are selected.
func (w *Screen) BuildItemMenu(in screen.Inputs, nest menu.NestedMenuBuilder) (menu.Menu, bool) {
	if w.list().IsSelecting() {
		deletion := w.chosenDeletion(in)
		if deletion.chosen() == 0 {
			return menu.Menu{}, false
		}

		return menu.WithNested(menu.Menu{Title: label.Count(deletion.chosen(), "worktree", "worktrees")}, "archive", w.deps.Keys.Menu.Archive, menu.Menu{
			Title: "archive",
			Entries: []menu.Entry{menu.Leaving("delete all", w.deps.Keys.ArchiveMenu.Delete, func(in screen.Inputs) screen.Request {
				return w.confirmDeleteChosen(in, deletion)
			})},
		}), true
	}

	row, ok := list.At(w.rows(in), w.Row)
	if !ok {
		return menu.Menu{}, false
	}

	if row.header {
		return menu.Menu{}, false
	}

	band := menu.Menu{Title: row.name()}
	if entry, ok := w.deleteEntry(row.item); ok {
		band = menu.WithNested(band, "archive", w.deps.Keys.Menu.Archive, menu.Menu{Title: "archive", Entries: []menu.Entry{entry}})
	}

	occupant, ok := row.item.FirstOccupant()
	if !ok {
		return band, len(band.Entries) > 0
	}

	to := screen.Target{AgentID: occupant.ID}
	if row.item.Task != nil {
		to.TaskID = row.item.Task.ID
	}

	return menu.WithNested(band, "jump", w.deps.Keys.Menu.Jump, nest.Jump(screen.Worktrees, to)), true
}

// BuildScreenMenu builds the worktrees screen's own band, for clearing every directory that can go.
func (w *Screen) BuildScreenMenu(in screen.Inputs, _ menu.NestedMenuBuilder) (menu.Menu, bool) {
	clearable := w.clearDeletion(in)
	clearAll := menu.Leaving("delete unused worktrees", w.deps.Keys.ArchiveMenu.Clear, func(in screen.Inputs) screen.Request {
		return w.confirmDeleteWorktrees(in, clearable)
	})
	clearAll.Off = clearable.isEmpty()

	return menu.Menu{
		Title:   "worktrees",
		Entries: []menu.Entry{clearAll},
	}, true
}

// deleteEntry is the entry that sends a directory the way its fate decides, false when
// the fate offers none.
func (w *Screen) deleteEntry(item panel.Worktree) (menu.Entry, bool) {
	desc := deleteDesc(item.Fate)
	run := func(in screen.Inputs) screen.Request { return w.deleteWorktree(in, item) }
	switch item.Fate {
	case panel.FateLive:
		return menu.Holding(desc, w.deps.Keys.ArchiveMenu.Archive, run), true
	case panel.FateArchived, panel.FateUnowned:
		return menu.Leaving(desc, w.deps.Keys.ArchiveMenu.Delete, run), true
	case panel.FateDeleting:
	}

	return menu.Entry{}, false
}
