package ui

import (
	"fmt"

	"komrad/internal/ui/cursor"
	"komrad/internal/ui/panel"

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
	rowCursor
	// drawn is the rows built for the frame once an update settles, and nil while one runs.
	drawn []scheduleItem
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

// stepKind moves the schedules view to its other kind with the cursor on the first row.
func (w *schedulesScreen) stepKind(m *model, delta int) tea.Cmd {
	left := m.leaveScreen()
	m.filter = filterState{}
	w.kind = schedulesKind(stepStop(int(w.kind), delta, len(w.kinds(m).labels)))
	if w.kind == schedulesArchived {
		w.archived.cursor = 0
		w.archived.stage(m)
	} else {
		w.cursor = 0
		m.stageTerminal()
	}

	return tea.Batch(left, m.waitToStage())
}

func (a *archivedSchedules) enter(m *model, _ screenTarget) tea.Cmd {
	a.stage(m)

	return m.waitToStage()
}

func (a *archivedSchedules) update(m *model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return a.list().update(m, key, a.actions())
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

func (a *archivedSchedules) stepKind(m *model, delta int) tea.Cmd {
	return m.schedules.stepKind(m, delta)
}

func (a *archivedSchedules) list() screenList[scheduleItem] {
	return screenList[scheduleItem]{
		cursor: func() *rowCursor { return &a.rowCursor },
		rows:   a.listRows,
		name:   func(item scheduleItem) string { return item.schedule.Name },
		placed: a.stage,
		moved: func(m *model) tea.Cmd {
			a.stage(m)

			return m.waitToStage()
		},
	}
}

// listRows is the archived schedules as drawn, or as they would be drawn when nothing has
// cached them.
func (a *archivedSchedules) listRows(m *model) []scheduleItem {
	if a.drawn != nil {
		return a.drawn
	}

	return a.buildRows(m)
}

func (a *archivedSchedules) buildRows(m *model) []scheduleItem {
	return narrowed(m.filter, narrowedParams[scheduleItem]{
		rows:  m.listing.archivedSchedules,
		depth: func(scheduleItem) int { return 0 },
		name:  func(item scheduleItem) string { return item.schedule.Name },
	})
}

// hoveredSchedule is the archived schedule the cursor is on.
func (a *archivedSchedules) hoveredSchedule(m *model) (panel.Schedule, bool) {
	item, ok := rowAt(a.listRows(m), a.cursor)

	return item.schedule, ok
}

// menuTarget is the archived schedule the archive menu would act on.
func (a *archivedSchedules) menuTarget(m *model) (archived, bool) {
	schedule, ok := a.hoveredSchedule(m)

	return archived{schedule: &schedule}, ok
}

func (a *archivedSchedules) buildItemMenu(m *model) (submenu, bool) {
	target, ok := a.menuTarget(m)
	if !ok {
		return submenu{}, false
	}

	return withNested(rowMenu(target.name(m.listing)), "archive", m.keys.Menu.Archive, archiveMenu(m, target)), true
}

// confirmDeleteSchedule asks before an archived schedule is deleted for good.
func confirmDeleteSchedule(m *model, schedule panel.Schedule) tea.Cmd {
	form, confirmed := deleteScheduleForm(schedule.Name, fixedKeys())

	return m.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			return m.awaitingWriteCmd(
				"deleting "+schedule.Name,
				fmt.Sprintf("Could not delete schedule %q", schedule.Name),
				&awaits{gone: rowsIn(listArchivedSchedules, schedule.ID)},
				func(c api) error { return c.deleteSchedule(schedule.ID) },
			)
		}),
	})
}

func (a *archivedSchedules) buildFrame(m *model) {
	a.drawn = a.buildRows(m)
}

// place keeps the cursor on the archived schedule it was on as the rows under it change.
func (a *archivedSchedules) place(m *model, hovered string) {
	rows := a.listRows(m)
	a.cursor = cursor.Place(rowIndexOfKey(rows, hovered), a.cursor, len(rows))
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archivedSchedules) stage(m *model) {
	m.stageTerminal()
}

func (a *archivedSchedules) listParams(m *model) screenListParams {
	return screenListParams{
		help: a.help,
		rows: len(a.listRows(m)),
		lines: func(pane listPane) ([]string, int) {
			return scheduleLines(m, pane, scheduleLinesParams{
				rows:   a.listRows(m),
				cursor: a.cursor,
				idle:   "no archived schedules",
			}), a.cursor
		},
	}
}

func (a *archivedSchedules) help(m *model) keyMap {
	return a.actions().help(m)
}

var (
	_ recordDrawer    = (*archivedSchedules)(nil)
	_ listFilterer    = (*archivedSchedules)(nil)
	_ rowStager       = (*archivedSchedules)(nil)
	_ hoveredNamer    = (*archivedSchedules)(nil)
	_ itemMenuBuilder = (*archivedSchedules)(nil)
	_ kindSwitcher    = (*archivedSchedules)(nil)
)

func (a *archivedSchedules) doesDrawFromListing() bool {
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

func (a *archivedSchedules) hoveredKey(m *model) string {
	return a.list().hovered(m)
}

func (a *archivedSchedules) hoveredName(m *model) string {
	return a.list().hoveredName(m)
}

func (a *archivedSchedules) placeAt(m *model, key string) {
	a.list().place(m, key)
}

func (a *archivedSchedules) filterRows(m *model) []filterRow {
	return a.list().filterRows(m)
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

func (a *archivedSchedules) dispatches(*model) []dispatching {
	return []dispatching{a.actions()}
}

func (a *archivedSchedules) stageResting(m *model) tea.Cmd {
	a.stage(m)

	return nil
}
