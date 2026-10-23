package schedules

import (
	"errors"
	"fmt"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/column"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

var errOpensOneAgent = errors.New("restore and open works on one agent at a time")

// kind is which schedules the schedules view is listing.
type kind int

const (
	live kind = iota
	archived
)

// archive is the schedules view on its archived kind: the cursor in the archived schedules.
type archive struct {
	deps Deps
	list.Cursor
	built screen.Derived[screen.ListingKey, []row]
}

// Kinds is the schedules view's row of kinds, live and archived.
func (w *Screen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels:      []string{"live", "archived"},
		FocusedStop: tabs.NoStop,
		ChosenStop:  int(w.kind),
	}
}

// KindsFocused is whether the row of kinds holds the keys.
func (w *Screen) KindsFocused() *bool {
	return &w.kindFocus
}

// KindsSwitch reports that the kind may always change.
func (w *Screen) KindsSwitch() bool {
	return true
}

// StepKind moves the schedules view to its other kind, back on the row it last stood on.
func (w *Screen) StepKind(in screen.Inputs, delta int) screen.Request {
	return screen.Leave{AfterClose: func() screen.Request {
		w.kind = kind(tabs.StepStop(int(w.kind), delta, len(w.Kinds().Labels)))

		return screen.Batch{screen.DropFilterLeaveCursor{}, screen.StageTerminal{}, screen.Settle{}}
	}}
}

// ShownArchive is the archived schedules as the screen that renders them, and false on the live kind.
func (w *Screen) ShownArchive() (Archive, bool) {
	if w.kind != archived {
		return Archive{}, false
	}

	return w.Archive(), true
}

// Archive is the schedules view's archive, its own state paired with the view whose kinds row it
// shares.
type Archive struct {
	*archive
	schedules *Screen
}

// Archive is the view's archive as the screen that renders it.
func (w *Screen) Archive() Archive {
	return Archive{archive: &w.archive, schedules: w}
}

// Kinds is the schedules view's row of kinds.
func (a Archive) Kinds() tabs.Stops {
	return a.schedules.Kinds()
}

// KindsFocused is whether the schedules view's row of kinds holds the keys.
func (a Archive) KindsFocused() *bool {
	return a.schedules.KindsFocused()
}

// KindsSwitch reports whether the schedules view's kind may change.
func (a Archive) KindsSwitch() bool {
	return a.schedules.KindsSwitch()
}

// StepKind moves the schedules view to its other kind.
func (a Archive) StepKind(in screen.Inputs, delta int) screen.Request {
	return a.schedules.StepKind(in, delta)
}

// Enter stages the archived schedule the cursor is on.
func (a Archive) Enter(in screen.Inputs, _ screen.Target) screen.Request {
	return screen.Batch{a.Stage(), screen.Settle{}}
}

// Update sends a key to the archive's table.
func (a Archive) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return a.actions(in).DispatchTop(a.deps.Top, a, a.deps.Keys, key.String())
}

// Receive keeps the cursor on its archived schedule as the listing moves.
func (a Archive) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	relisted, ok := msg.(screen.ListingChanged)
	if !ok {
		return nil, false
	}

	a.place(in, a.list().Hovered(relisted.WithPreviousListing(in)))

	return nil, true
}

func (a Archive) list() keys.ScreenList[row] {
	return keys.ScreenList[row]{
		Keys:   a.deps.Keys,
		Cursor: func() *list.Cursor { return &a.Cursor },
		Rows:   a.listRows,
		Name:   rowName,
		Fits:   rowFits,
		Placed: func(screen.Inputs) screen.Request { return a.Stage() },
		Moved:  func(screen.Inputs) screen.Request { return screen.Batch{a.Stage(), screen.Settle{}} },
	}
}

func (a Archive) listRows(in screen.Inputs) []row {
	return a.built.Read(in.ListingKey(), func() []row {
		return list.Narrowed(in.Room.Pattern, list.NarrowedParams[row]{
			Rows:  xslice.Map(in.Listing.ArchivedSchedules, rowOf),
			Depth: func(row) int { return 0 },
			Name:  rowName,
			Fits:  rowFits(in),
		})
	})
}

// hoveredSchedule is the archived schedule the cursor is on.
func (a Archive) hoveredSchedule(in screen.Inputs) (panel.Schedule, bool) {
	item, ok := list.At(a.listRows(in), a.Row)

	return item.schedule, ok
}

// BuildItemMenu builds the band on the hovered archived schedule.
func (a Archive) BuildItemMenu(in screen.Inputs, _ menu.NestedMenuBuilder) (menu.Menu, bool) {
	schedule, ok := a.hoveredSchedule(in)
	if !ok {
		return menu.Menu{}, false
	}

	return menu.WithNested(menu.Menu{Title: schedule.Name}, "archive", a.deps.Keys.Menu.Archive, a.archiveMenu(schedule)), true
}

// archiveMenu is the archive's actions on an archived schedule, opened from its menu.
func (a Archive) archiveMenu(schedule panel.Schedule) menu.Menu {
	bindings := a.deps.Keys.ArchiveMenu

	return menu.Menu{
		Title: "archive",
		Entries: []menu.Entry{
			menu.Holding("restore", bindings.Restore, func(screen.Inputs) screen.Request { return a.restore(schedule) }),
			{
				Label:     "restore and open",
				Keys:      bindings.RestoreOpen,
				Off:       true,
				AfterPick: menu.Hold,
				Pick: func(screen.Inputs) screen.Request {
					return screen.Report{Summary: "Could not restore and open " + schedule.Name, Err: errOpensOneAgent}
				},
			},
			menu.Leaving("delete for good", bindings.Delete, func(screen.Inputs) screen.Request {
				return confirmDelete(a.deps.Clients, schedule)
			}),
		},
	}
}

func (a Archive) restore(schedule panel.Schedule) screen.Request {
	return a.deps.Clients.AwaitingWrite(api.AwaitingWriteParams{
		Doing:          "restoring " + schedule.Name,
		FailureSummary: fmt.Sprintf("Could not restore schedule %q", schedule.Name),
		WaitFor:        screen.WaitFor{RowsGone: screen.RowsIn(screen.ListArchivedSchedules, schedule.ID)},
		Write:          func(c api.Clients) error { return c.RestoreSchedule(schedule.ID) },
	})
}

// confirmDelete asks before it deletes an archived schedule for good.
func confirmDelete(clients api.Clients, schedule panel.Schedule) screen.Request {
	built, confirmed := deleteForm(schedule.Name, config.Fixed())

	return form.Open{Form: form.Form{
		Prebuilt: built,
		Submit: func(form.Inputs) screen.Request {
			if !*confirmed {
				return nil
			}

			return clients.AwaitingWrite(api.AwaitingWriteParams{
				Doing:          "deleting " + schedule.Name,
				FailureSummary: fmt.Sprintf("Could not delete schedule %q", schedule.Name),
				WaitFor:        screen.WaitFor{RowsGone: screen.RowsIn(screen.ListArchivedSchedules, schedule.ID)},
				Write:          func(c api.Clients) error { return c.DeleteSchedule(schedule.ID) },
			})
		},
		Narrow:          true,
		ClosesOnRefusal: true,
	}}
}

// place keeps the cursor on the archived schedule it was on as the rows under it change.
func (a Archive) place(in screen.Inputs, hovered string) {
	rows := a.listRows(in)
	a.Row = cursor.Place(list.IndexOf(rows, hovered), a.Row, len(rows))
}

// Stage keeps the info pane on the row the cursor is standing on.
func (a Archive) Stage() screen.Request {
	return screen.StageTerminal{}
}

// ListParams is the list of archived schedules as the root renders it.
func (a Archive) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Help:  a.help(in),
		Rows:  len(a.listRows(in)),
		Empty: "no archived schedules",
		Snapshot: func(pane list.Pane) view.Snapshot {
			return listSnapshot(in, pane, listSnapshotParams{
				rows:   a.listRows(in),
				cursor: a.Row,
			})
		},
	}
}

func (a Archive) help(in screen.Inputs) legend.Help {
	return a.actions(in).Help(a, a.deps.Keys)
}

// DoesRenderFromListing reports that the archive renders the listing's archived schedules.
func (a Archive) DoesRenderFromListing() bool {
	return true
}

// TakesEveryKey reports that nothing on the archive takes every key.
func (a Archive) TakesEveryKey() bool {
	return false
}

// ActiveList is the list of archived schedules the filter narrows.
func (a Archive) ActiveList() keys.KeyedList {
	return a.list()
}

// HoveredName is the name of the archived schedule under the cursor.
func (a Archive) HoveredName(in screen.Inputs) string {
	return a.list().HoveredName(in)
}

// Column is the hovered archived schedule's document rendered beside the list.
func (a Archive) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return a.docRows(in, width)
		},
	}
}

// Dispatches is the archive's one table.
func (a Archive) Dispatches() []keys.Table {
	return []keys.Table{a.actions(screen.Inputs{})}
}

// StageResting stages the pane for the archived schedule the cursor rests on.
func (a Archive) StageResting(in screen.Inputs) screen.Request {
	return a.Stage()
}
