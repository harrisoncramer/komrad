package ui

import (
	"fmt"

	"komrad/internal/ui/cursor"
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// archiveScreen is the archive screen: the cursor in the archived schedules.
type archiveScreen struct {
	schedules      rowCursor
	drawnSchedules []scheduleItem
}

func (a *archiveScreen) enter(m *model, _ screenTarget) tea.Cmd {
	a.stage(m)

	return m.waitToStage()
}

func (a *archiveScreen) update(m *model, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return a.scheduleList().update(m, key, a.actions())
}

func (a *archiveScreen) scheduleList() screenList[scheduleItem] {
	return screenList[scheduleItem]{
		cursor: func() *rowCursor { return &a.schedules },
		rows:   a.scheduleListRows,
		name:   func(item scheduleItem) string { return item.schedule.Name },
		placed: a.stage,
		moved: func(m *model) tea.Cmd {
			a.stage(m)

			return m.waitToStage()
		},
	}
}

// scheduleListRows is the archived schedules as drawn, or as they would be drawn when nothing has
// cached them.
func (a *archiveScreen) scheduleListRows(m *model) []scheduleItem {
	if a.drawnSchedules != nil {
		return a.drawnSchedules
	}

	return a.buildScheduleRows(m)
}

func (a *archiveScreen) buildScheduleRows(m *model) []scheduleItem {
	return narrowed(m.filter, narrowedParams[scheduleItem]{
		rows:  m.listing.archivedSchedules,
		depth: func(scheduleItem) int { return 0 },
		name:  func(item scheduleItem) string { return item.schedule.Name },
	})
}

// hoveredSchedule is the archived schedule the cursor is on.
func (a *archiveScreen) hoveredSchedule(m *model) (panel.Schedule, bool) {
	item, ok := rowAt(a.scheduleListRows(m), a.schedules.cursor)

	return item.schedule, ok
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

func (a *archiveScreen) buildFrame(m *model) {
	a.drawnSchedules = a.buildScheduleRows(m)
}

// placeSchedule keeps the cursor on the archived schedule it was on as the rows under it change.
func (a *archiveScreen) placeSchedule(m *model, hovered string) {
	rows := a.scheduleListRows(m)
	a.schedules.cursor = cursor.Place(rowIndexOfKey(rows, hovered), a.schedules.cursor, len(rows))
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archiveScreen) stage(m *model) {
	m.stageTerminal()
}

func (a *archiveScreen) listParams(m *model) screenListParams {
	return screenListParams{
		help: a.help,
		rows: len(a.scheduleListRows(m)),
		lines: func(pane listPane) ([]string, int) {
			return scheduleLines(m, pane, scheduleLinesParams{
				rows:   a.scheduleListRows(m),
				cursor: a.schedules.cursor,
				idle:   "no archived schedules",
			}), a.schedules.cursor
		},
	}
}

func (a *archiveScreen) help(m *model) keyMap {
	return a.actions().help(m)
}

var (
	_ recordDrawer    = (*archiveScreen)(nil)
	_ listFilterer    = (*archiveScreen)(nil)
	_ rowStager       = (*archiveScreen)(nil)
	_ hoveredNamer    = (*archiveScreen)(nil)
	_ itemMenuBuilder = (*archiveScreen)(nil)
)

func (a *archiveScreen) doesDrawFromListing() bool {
	return true
}

func (a *archiveScreen) takesEveryKey() bool {
	return false
}

func (a *archiveScreen) detailsFit(m *model) bool {
	return m.listFitsDetails(a.listParams(m))
}

func (a *archiveScreen) detailsRoom(m *model) int {
	return m.listRecordRoom(a.listParams(m))
}

func (a *archiveScreen) hoveredKey(m *model) string {
	return a.scheduleList().hovered(m)
}

func (a *archiveScreen) hoveredName(m *model) string {
	return a.scheduleList().hoveredName(m)
}

func (a *archiveScreen) placeAt(m *model, key string) {
	a.scheduleList().place(m, key)
}

func (a *archiveScreen) filterRows(m *model) []filterRow {
	return a.scheduleList().filterRows(m)
}

func (a *archiveScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		read:  isAlwaysRead,
		docRows: func(m *model, width int) []string {
			return m.withLastScreen(m.archive.docRows(m, width), width)
		},
	}
}

func (a *archiveScreen) dispatches(*model) []dispatching {
	return []dispatching{a.actions()}
}

func (a *archiveScreen) stageResting(m *model) tea.Cmd {
	a.stage(m)

	return nil
}
