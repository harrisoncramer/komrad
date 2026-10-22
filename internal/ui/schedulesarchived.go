package ui

import (
	"fmt"

	"komrad/internal/ui/api"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
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

func (w *schedulesScreen) kinds(*model) stops {
	return stops{
		labels: []string{"live", "archived"},
		focus:  noStop,
		lit:    int(w.kind),
	}
}

func (w *schedulesScreen) kindsFocused(*model) *bool {
	return &w.kindFocus
}

func (w *schedulesScreen) kindsFit(m *model) bool {
	return m.listFitsTabs(w.listParams(m))
}

func (w *schedulesScreen) kindsSwitch(*model) bool {
	return true
}

// stepKind moves the schedules view to its other kind, back on the row it last stood on.
func (w *schedulesScreen) stepKind(m *model, delta int) screen.Request {
	return screen.Leave{Then: func() screen.Request {
		w.kind = schedulesKind(stepStop(int(w.kind), delta, len(w.kinds(m).labels)))

		return screen.Batch{screen.ResetFilter{}, screen.StageTerminal{}, screen.Settle{}}
	}}
}

func (a *archivedSchedules) enter(m *model, _ screen.Target) screen.Request {
	return screen.Batch{a.stage(m), screen.Settle{}}
}

func (a *archivedSchedules) update(m *model, msg tea.Msg) screen.Request {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return a.list().update(m, key, a.actions())
}

func (a *archivedSchedules) receive(*model, tea.Msg) (screen.Request, bool) {
	return nil, false
}

func (a *archivedSchedules) kinds(m *model) stops {
	return m.schedules.kinds(m)
}

func (a *archivedSchedules) kindsFocused(m *model) *bool {
	return m.schedules.kindsFocused(m)
}

func (a *archivedSchedules) kindsFit(m *model) bool {
	return m.listFitsTabs(a.listParams(m))
}

func (a *archivedSchedules) kindsSwitch(*model) bool {
	return true
}

func (a *archivedSchedules) stepKind(m *model, delta int) screen.Request {
	return m.schedules.stepKind(m, delta)
}

func (a *archivedSchedules) list() screenList[scheduleItem] {
	return screenList[scheduleItem]{
		keys:   a.deps.keys,
		top:    a.deps.top,
		cursor: func() *list.Cursor { return &a.Cursor },
		rows:   a.listRows,
		name:   func(item scheduleItem) string { return item.schedule.Name },
		fits:   (*model).scheduleItemFits,
		placed: a.stage,
		moved:  func(m *model) screen.Request { return screen.Batch{a.stage(m), screen.Settle{}} },
	}
}

func (a *archivedSchedules) listRows(m *model) []scheduleItem {
	return a.built.read(m.listingKey(), func() []scheduleItem {
		return narrowed(m.filter, narrowedParams[scheduleItem]{
			rows:  xslice.Map(m.listing.ArchivedSchedules, scheduleItemOf),
			depth: func(scheduleItem) int { return 0 },
			name:  func(item scheduleItem) string { return item.schedule.Name },
			fits:  m.scheduleItemFits,
		})
	})
}

// hoveredSchedule is the archived schedule the cursor is on.
func (a *archivedSchedules) hoveredSchedule(m *model) (panel.Schedule, bool) {
	item, ok := list.At(a.listRows(m), a.Row)

	return item.schedule, ok
}

// menuTarget is the archived schedule the archive menu would act on.
func (a *archivedSchedules) menuTarget(m *model) (archived, bool) {
	schedule, ok := a.hoveredSchedule(m)

	return archived{schedule: &schedule}, ok
}

func (a *archivedSchedules) buildItemMenu(m *model) (menu.Menu, bool) {
	target, ok := a.menuTarget(m)
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
func (a *archivedSchedules) place(m *model, hovered string) {
	rows := a.listRows(m)
	a.Row = cursor.Place(list.IndexOf(rows, hovered), a.Row, len(rows))
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archivedSchedules) stage(*model) screen.Request {
	return screen.StageTerminal{}
}

func (a *archivedSchedules) listParams(m *model) screenListParams {
	return screenListParams{
		help:  a.help,
		rows:  len(a.listRows(m)),
		empty: "no archived schedules",
		snapshot: func(pane list.Pane) view.Snapshot {
			return scheduleSnapshot(m, pane, scheduleSnapshotParams{
				rows:   a.listRows(m),
				cursor: a.Row,
			})
		},
	}
}

func (a *archivedSchedules) help(m *model) keyMap {
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

func (a *archivedSchedules) detailsFit(m *model) bool {
	return m.listFitsDetails(a.listParams(m))
}

func (a *archivedSchedules) detailsRoom(m *model) int {
	return m.listRecordRoom(a.listParams(m))
}

func (a *archivedSchedules) activeList() keyedList {
	return a.list()
}

func (a *archivedSchedules) hoveredName(m *model) string {
	return a.list().hoveredName(m)
}

func (a *archivedSchedules) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		read:  isAlwaysRead,
		docRows: func(m *model, width int) []string {
			return a.docRows(m, width)
		},
	}
}

func (a *archivedSchedules) dispatches(*model) []keys.Table {
	return []keys.Table{a.actions()}
}

func (a *archivedSchedules) stageResting(m *model) screen.Request {
	return a.stage(m)
}
