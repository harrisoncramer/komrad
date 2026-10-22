package ui

import (
	"fmt"

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

// schedulesKind is which schedules the schedules view is listing.
type schedulesKind int

const (
	schedulesLive schedulesKind = iota
	schedulesArchived
)

// archivedSchedules is the schedules view on its archived kind: the cursor in the archived schedules.
type archivedSchedules struct {
	deps schedulesDeps
	list.Cursor
	built derived[listingKey, []scheduleItem]
}

func (w *schedulesScreen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels: []string{"live", "archived"},
		Focus:  tabs.NoStop,
		Lit:    int(w.kind),
	}
}

func (w *schedulesScreen) KindsFocused() *bool {
	return &w.kindFocus
}

func (w *schedulesScreen) KindsSwitch() bool {
	return true
}

// StepKind moves the schedules view to its other kind, back on the row it last stood on.
func (w *schedulesScreen) StepKind(in screen.Inputs, delta int) screen.Request {
	return screen.Leave{Then: func() screen.Request {
		w.kind = schedulesKind(tabs.StepStop(int(w.kind), delta, len(w.Kinds().Labels)))

		return screen.Batch{screen.ResetFilter{}, screen.StageTerminal{}, screen.Settle{}}
	}}
}

// ShownArchive is the archived schedules as the screen that renders them, and false on the live kind.
func (w *schedulesScreen) ShownArchive() (archivedSchedulesScreen, bool) {
	if w.kind != schedulesArchived {
		return archivedSchedulesScreen{}, false
	}

	return w.archiveScreen(), true
}

// archivedSchedulesScreen is the schedules view's archive, its own state paired with the view whose
// kinds row it shares.
type archivedSchedulesScreen struct {
	*archivedSchedules
	schedules *schedulesScreen
}

// archiveScreen is the view's archive as the screen that renders it.
func (w *schedulesScreen) archiveScreen() archivedSchedulesScreen {
	return archivedSchedulesScreen{archivedSchedules: &w.archived, schedules: w}
}

func (s archivedSchedulesScreen) Kinds() tabs.Stops {
	return s.schedules.Kinds()
}

func (s archivedSchedulesScreen) KindsFocused() *bool {
	return s.schedules.KindsFocused()
}

func (s archivedSchedulesScreen) KindsSwitch() bool {
	return s.schedules.KindsSwitch()
}

func (s archivedSchedulesScreen) StepKind(in screen.Inputs, delta int) screen.Request {
	return s.schedules.StepKind(in, delta)
}

var _ kindSwitcher = archivedSchedulesScreen{}

func (a *archivedSchedules) Enter(in screen.Inputs, _ screen.Target) screen.Request {
	return screen.Batch{a.Stage(), screen.Settle{}}
}

func (a *archivedSchedules) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return a.actions(in).DispatchTop(a.deps.top, a, a.deps.keys, key.String())
}

func (a *archivedSchedules) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	relisted, ok := msg.(screen.Relisted)
	if !ok {
		return nil, false
	}

	a.place(in, a.list().Hovered(relisted.Before(in)))

	return nil, true
}

func (a *archivedSchedules) list() keys.ScreenList[scheduleItem] {
	return keys.ScreenList[scheduleItem]{
		Keys:   a.deps.keys,
		Cursor: func() *list.Cursor { return &a.Cursor },
		Rows:   a.listRows,
		Name:   func(item scheduleItem) string { return item.schedule.Name },
		Fits:   scheduleItemFits,
		Placed: func(screen.Inputs) screen.Request { return a.Stage() },
		Moved:  func(screen.Inputs) screen.Request { return screen.Batch{a.Stage(), screen.Settle{}} },
	}
}

func (a *archivedSchedules) listRows(in screen.Inputs) []scheduleItem {
	return a.built.read(listingKeyOf(in), func() []scheduleItem {
		return list.Narrowed(in.Room.Pattern, list.NarrowedParams[scheduleItem]{
			Rows:  xslice.Map(in.Listing.ArchivedSchedules, scheduleItemOf),
			Depth: func(scheduleItem) int { return 0 },
			Name:  func(item scheduleItem) string { return item.schedule.Name },
			Fits:  scheduleItemFits(in),
		})
	})
}

// hoveredSchedule is the archived schedule the cursor is on.
func (a *archivedSchedules) hoveredSchedule(in screen.Inputs) (panel.Schedule, bool) {
	item, ok := list.At(a.listRows(in), a.Row)

	return item.schedule, ok
}

// menuTarget is the archived schedule the archive menu would act on.
func (a *archivedSchedules) menuTarget(in screen.Inputs) (archived, bool) {
	schedule, ok := a.hoveredSchedule(in)

	return archived{schedule: &schedule}, ok
}

func (a *archivedSchedules) BuildItemMenu(in screen.Inputs, _ menu.Nester) (menu.Menu, bool) {
	target, ok := a.menuTarget(in)
	if !ok {
		return menu.Menu{}, false
	}

	return menu.WithNested(rowMenu(target.name()), "archive", a.deps.keys.Menu.Archive, archiveMenu(in, a.deps.keys, a.deps.clients, target)), true
}

// confirmDeleteSchedule asks before it deletes an archived schedule for good.
func confirmDeleteSchedule(clients api.Clients, schedule panel.Schedule) screen.Request {
	built, confirmed := deleteScheduleForm(schedule.Name, fixedKeys())

	return form.Open{Form: form.Form{
		Built: built,
		Submit: func(form.Inputs) screen.Request {
			if !*confirmed {
				return nil
			}

			return clients.AwaitingWrite(api.AwaitingWriteParams{
				Doing:   "deleting " + schedule.Name,
				Summary: fmt.Sprintf("Could not delete schedule %q", schedule.Name),
				Awaits:  screen.Awaits{Gone: screen.RowsIn(screen.ListArchivedSchedules, schedule.ID)},
				Write:   func(c api.Clients) error { return c.DeleteSchedule(schedule.ID) },
			})
		},
		Narrow:          true,
		ClosesOnRefusal: true,
	}}
}

// place keeps the cursor on the archived schedule it was on as the rows under it change.
func (a *archivedSchedules) place(in screen.Inputs, hovered string) {
	rows := a.listRows(in)
	a.Row = cursor.Place(list.IndexOf(rows, hovered), a.Row, len(rows))
}

// Stage keeps the info pane on the row the cursor is standing on.
func (a *archivedSchedules) Stage() screen.Request {
	return screen.StageTerminal{}
}

func (a *archivedSchedules) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Help:  a.help(in),
		Rows:  len(a.listRows(in)),
		Empty: "no archived schedules",
		Snapshot: func(pane list.Pane) view.Snapshot {
			return scheduleSnapshot(in, pane, scheduleSnapshotParams{
				rows:   a.listRows(in),
				cursor: a.Row,
			})
		},
	}
}

func (a *archivedSchedules) help(in screen.Inputs) legend.Help {
	return a.actions(in).Help(a, a.deps.keys)
}

var (
	_ recordRenderer  = (*archivedSchedules)(nil)
	_ activeLister    = (*archivedSchedules)(nil)
	_ rowStager       = (*archivedSchedules)(nil)
	_ hoveredNamer    = (*archivedSchedules)(nil)
	_ itemMenuBuilder = (*archivedSchedules)(nil)
)

func (a *archivedSchedules) DoesRenderFromListing() bool {
	return true
}

func (a *archivedSchedules) TakesEveryKey() bool {
	return false
}

func (a *archivedSchedules) ActiveList() keys.KeyedList {
	return a.list()
}

func (a *archivedSchedules) HoveredName(in screen.Inputs) string {
	return a.list().HoveredName(in)
}

func (a *archivedSchedules) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return a.docRows(in, width)
		},
	}
}

func (a *archivedSchedules) Dispatches() []keys.Table {
	return []keys.Table{a.actions(screen.Inputs{})}
}

func (a *archivedSchedules) StageResting(in screen.Inputs) screen.Request {
	return a.Stage()
}
