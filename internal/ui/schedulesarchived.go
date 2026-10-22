package ui

import (
	"fmt"

	"komrad/internal/ui/cursor"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/view"

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
func (w *schedulesScreen) stepKind(m *model, delta int) tea.Cmd {
	left := m.leaveScreen()
	m.filter = filterState{}
	w.kind = schedulesKind(stepStop(int(w.kind), delta, len(w.kinds(m).labels)))
	if w.kind == schedulesArchived {
		w.archived.stage(m)
	} else {
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
		cursor: func() *list.Cursor { return &a.Cursor },
		rows:   a.listRows,
		name:   func(item scheduleItem) string { return item.schedule.Name },
		fits:   (*model).scheduleItemFits,
		placed: a.stage,
		moved: func(m *model) tea.Cmd {
			a.stage(m)

			return m.waitToStage()
		},
	}
}

func (a *archivedSchedules) listRows(m *model) []scheduleItem {
	return a.built.read(m.listingKey(), func() []scheduleItem {
		return narrowed(m.filter, narrowedParams[scheduleItem]{
			rows:  m.listing.archivedSchedules,
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

func (a *archivedSchedules) buildItemMenu(m *model) (submenu, bool) {
	target, ok := a.menuTarget(m)
	if !ok {
		return submenu{}, false
	}

	return withNested(rowMenu(target.name()), "archive", m.keys.Menu.Archive, archiveMenu(m, target)), true
}

// confirmDeleteSchedule asks before it deletes an archived schedule for good.
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

// place keeps the cursor on the archived schedule it was on as the rows under it change.
func (a *archivedSchedules) place(m *model, hovered string) {
	rows := a.listRows(m)
	a.Row = cursor.Place(list.IndexOf(rows, hovered), a.Row, len(rows))
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archivedSchedules) stage(m *model) {
	m.stageTerminal()
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
	return a.actions().help(m)
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

func (a *archivedSchedules) dispatches(*model) []dispatching {
	return []dispatching{a.actions()}
}

func (a *archivedSchedules) stageResting(m *model) tea.Cmd {
	a.stage(m)

	return nil
}
