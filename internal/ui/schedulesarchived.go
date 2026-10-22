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

func (w *schedulesScreen) kinds(*model) tabs.Stops {
	return tabs.Stops{
		Labels: []string{"live", "archived"},
		Focus:  tabs.NoStop,
		Lit:    int(w.kind),
	}
}

func (w *schedulesScreen) kindsFocused(*model) *bool {
	return &w.kindFocus
}

func (w *schedulesScreen) kindsSwitch(*model) bool {
	return true
}

// stepKind moves the schedules view to its other kind, back on the row it last stood on.
func (w *schedulesScreen) stepKind(m *model, in screen.Inputs, delta int) screen.Request {
	return screen.Leave{Then: func() screen.Request {
		w.kind = schedulesKind(tabs.StepStop(int(w.kind), delta, len(w.kinds(m).Labels)))

		return screen.Batch{screen.ResetFilter{}, screen.StageTerminal{}, screen.Settle{}}
	}}
}

func (a *archivedSchedules) enter(m *model, in screen.Inputs, _ screen.Target) screen.Request {
	return screen.Batch{a.stage(m, in), screen.Settle{}}
}

func (a *archivedSchedules) update(m *model, in screen.Inputs, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return a.list().update(m, key, a.actions())
}

func (a *archivedSchedules) receive(m *model, in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	relisted, ok := msg.(screen.Relisted)
	if !ok {
		return nil, false
	}

	a.place(m, in, a.list().hovered(m, relisted.Before(in)))

	return nil, true
}

func (a *archivedSchedules) kinds(m *model) tabs.Stops {
	return m.schedules.kinds(m)
}

func (a *archivedSchedules) kindsFocused(m *model) *bool {
	return m.schedules.kindsFocused(m)
}

func (a *archivedSchedules) kindsSwitch(*model) bool {
	return true
}

func (a *archivedSchedules) stepKind(m *model, in screen.Inputs, delta int) screen.Request {
	return m.schedules.stepKind(m, in, delta)
}

func (a *archivedSchedules) list() screenList[scheduleItem] {
	return screenList[scheduleItem]{
		keys:   a.deps.keys,
		top:    a.deps.top,
		cursor: func() *list.Cursor { return &a.Cursor },
		rows:   a.listRows,
		name:   func(item scheduleItem) string { return item.schedule.Name },
		fits:   scheduleItemFits,
		placed: a.stage,
		moved:  func(m *model, in screen.Inputs) screen.Request { return screen.Batch{a.stage(m, in), screen.Settle{}} },
	}
}

func (a *archivedSchedules) listRows(m *model, in screen.Inputs) []scheduleItem {
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
func (a *archivedSchedules) hoveredSchedule(m *model, in screen.Inputs) (panel.Schedule, bool) {
	item, ok := list.At(a.listRows(m, in), a.Row)

	return item.schedule, ok
}

// menuTarget is the archived schedule the archive menu would act on.
func (a *archivedSchedules) menuTarget(m *model, in screen.Inputs) (archived, bool) {
	schedule, ok := a.hoveredSchedule(m, in)

	return archived{schedule: &schedule}, ok
}

func (a *archivedSchedules) buildItemMenu(m *model, in screen.Inputs) (menu.Menu, bool) {
	target, ok := a.menuTarget(m, in)
	if !ok {
		return menu.Menu{}, false
	}

	return menu.WithNested(rowMenu(target.name()), "archive", a.deps.keys.Menu.Archive, archiveMenu(m, a.deps.keys, a.deps.clients, target)), true
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
func (a *archivedSchedules) place(m *model, in screen.Inputs, hovered string) {
	rows := a.listRows(m, in)
	a.Row = cursor.Place(list.IndexOf(rows, hovered), a.Row, len(rows))
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archivedSchedules) stage(*model, screen.Inputs) screen.Request {
	return screen.StageTerminal{}
}

func (a *archivedSchedules) listParams(m *model, in screen.Inputs) list.Params {
	return list.Params{
		Help:  a.help(m),
		Rows:  len(a.listRows(m, in)),
		Empty: "no archived schedules",
		Snapshot: func(pane list.Pane) view.Snapshot {
			return scheduleSnapshot(in, pane, scheduleSnapshotParams{
				rows:   a.listRows(m, in),
				cursor: a.Row,
			})
		},
	}
}

func (a *archivedSchedules) help(m *model) legend.Help {
	return m.helpOf(a.actions())
}

var (
	_ recordRenderer  = (*archivedSchedules)(nil)
	_ activeLister    = (*archivedSchedules)(nil)
	_ rowStager       = (*archivedSchedules)(nil)
	_ hoveredNamer    = (*archivedSchedules)(nil)
	_ itemMenuBuilder = (*archivedSchedules)(nil)
	_ kindSwitcher    = (*archivedSchedules)(nil)
)

func (a *archivedSchedules) doesRenderFromListing() bool {
	return true
}

func (a *archivedSchedules) takesEveryKey() bool {
	return false
}

func (a *archivedSchedules) activeList() keyedList {
	return a.list()
}

func (a *archivedSchedules) hoveredName(m *model, in screen.Inputs) string {
	return a.list().hoveredName(m, in)
}

func (a *archivedSchedules) column(m *model, in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: columnFloor,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return a.docRows(m, in, width)
		},
	}
}

func (a *archivedSchedules) dispatches(*model) []keys.Table {
	return []keys.Table{a.actions()}
}

func (a *archivedSchedules) stageResting(m *model, in screen.Inputs) screen.Request {
	return a.stage(m, in)
}
