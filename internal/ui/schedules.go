package ui

import (
	"fmt"
	"time"

	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/paint"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type scheduleState struct {
	items  []scheduleItem
	cursor int
}

type scheduleItem struct {
	schedule schedulestore.Schedule
	nextRun  time.Time
}

func (m model) enterSchedules() (tea.Model, tea.Cmd) {
	m.enterScreen(screenSchedules)
	rows := m.scheduleRows()
	m.landScheduleCursor(rows, clampCursor(m.schedules.cursor, len(rows)))

	return m, nil
}

func (m model) moveScheduleCursor(delta int) model {
	rows := m.scheduleRows()
	if next, ok := stepCursor(m.schedules.cursor, delta, len(rows)); ok {
		m.landScheduleCursor(rows, next)
	}

	return m
}

func (m model) scheduleRows() []scheduleItem {
	return narrowed(m.filter, narrowedParams[scheduleItem]{
		rows:  m.schedules.items,
		depth: func(scheduleItem) int { return 0 },
		name:  func(item scheduleItem) string { return item.schedule.Name },
	})
}

// scheduleFilterRows is the list's own rows with the name a search reads each by.
func (m model) scheduleFilterRows() []filterRow {
	return xslice.Map(m.scheduleRows(), func(item scheduleItem) filterRow {
		return filterRow{key: item.schedule.ID, name: item.schedule.Name}
	})
}

func (m model) updateSchedules(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return schedulesActions().dispatch(m, key.String())
}

func (m model) selectedSchedule() (scheduleItem, bool) {
	return rowAt(m.scheduleRows(), m.schedules.cursor)
}

// hoveredScheduleKey is the row the cursor is on, read before a filter narrows the list under
// it.
func (m model) hoveredScheduleKey() string {
	item, ok := m.selectedSchedule()
	if !ok {
		return ""
	}

	return item.schedule.ID
}

func (m *model) placeScheduleCursor(hovered string) {
	rows := m.scheduleRows()

	found := -1
	for i, item := range rows {
		if item.schedule.ID == hovered {
			found = i
		}
	}

	m.landScheduleCursor(rows, placeCursor(found, m.schedules.cursor, len(rows)))
}

func (m model) schedulesDetails() details {
	item, ok := m.selectedSchedule()
	if !ok {
		return details{}
	}

	return details{
		id:     item.schedule.ID,
		fields: scheduleFields(item.schedule, m.workflowByID(item.schedule.WorkflowID)),
	}
}

// schedulesColumn is the hovered schedule's own record drawn beside the list.
func schedulesColumn() columnSpec {
	return columnSpec{floor: columnFloor, doc: model.scheduleDoc}
}

// scheduleDoc is the markdown drawn beside the list: the notice for an empty list, or the
// schedule the cursor is on.
func (m model) scheduleDoc() string {
	item, ok := m.selectedSchedule()
	if !ok {
		return noSchedulesMarkdown
	}

	return scheduleMarkdown(item)
}

func (m model) workflowByID(id string) *workflowstore.Workflow {
	for _, w := range m.listing.workflows {
		if w.ID == id {
			return &w
		}
	}

	return nil
}

func (m model) toggleSchedule() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSchedule()
	if !ok {
		return m, nil
	}

	failure := fmt.Sprintf("Could not pause schedule %q", item.schedule.Name)
	if !item.schedule.Enabled {
		failure = fmt.Sprintf("Could not resume schedule %q", item.schedule.Name)
	}

	return m, m.writeCmd(failure, func(a api) error {
		return a.setScheduleEnabled(item.schedule.ID, !item.schedule.Enabled)
	})
}

func (m model) newScheduleForm() (tea.Model, tea.Cmd) {
	if len(m.listing.workflows) == 0 {
		m.report("Could not create a schedule", errNoWorkflow)

		return m, nil
	}

	values := schedulestore.Config{
		Enabled:    true,
		WorkflowID: m.listing.workflows[0].ID,
	}

	return m.openScheduleForm(&values, nil)
}

// openScheduleForm puts up the schedule form, creating a schedule when target is nil and saving
// over the one it names otherwise.
func (m model) openScheduleForm(values *schedulestore.Config, target *schedulestore.Schedule) (tea.Model, tea.Cmd) {
	return m.openRedrawnForm(openRedrawnFormParams{
		Kind: formSchedule,
		Build: func(m model) (*huh.Form, []string) {
			return scheduleForm(values, m.listing.workflows, formKeys())
		},
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit: func(m model) (tea.Model, tea.Cmd) {
			var cmd tea.Cmd
			if target == nil {
				cmd = m.writeCmd("Could not create the schedule", func(a api) error { return a.createSchedule(*values) })
			} else {
				cmd = m.writeCmd(
					fmt.Sprintf("Could not save schedule %q", target.Name),
					func(a api) error { return a.updateSchedule(target.ID, *values) },
				)
			}

			return m, tea.Batch(cmd, m.holdForm())
		},
	})
}

func (m model) editScheduleForm() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSchedule()
	if !ok {
		return m, nil
	}

	target := item.schedule
	if fresh, err := m.api.getSchedule(target.ID); err == nil && fresh != nil {
		target = *fresh
	}

	values := schedulePrefill(target)

	return m.openScheduleForm(&values, &target)
}

// schedulePrefill is the schedule form as the stored schedule leaves it.
func schedulePrefill(schedule schedulestore.Schedule) schedulestore.Config {
	return schedulestore.Config{
		Name:       schedule.Name,
		Cron:       schedule.Cron,
		Timezone:   schedule.Timezone,
		Enabled:    schedule.Enabled,
		HideAgents: schedule.HideAgents,
		Prompt:     schedule.Prompt,
		WorkflowID: schedule.WorkflowID,
	}
}

func (m model) archiveScheduleConfirm() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSchedule()
	if !ok {
		return m, nil
	}

	target := item.schedule

	form, confirmed := archiveScheduleForm(target.Name, formKeys())

	return m, m.openForm(formState{
		kind:   formScheduleArchive,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.writeCmd(
				fmt.Sprintf("Could not archive schedule %q", target.Name),
				func(a api) error { return a.archiveSchedule(target.ID) },
			)
		}),
	})
}

// landScheduleCursor puts the cursor on a row, naming the row rather than only its place in the
// list so the record's own offset is kept.
func (m *model) landScheduleCursor(rows []scheduleItem, idx int) {
	m.schedules.cursor = idx

	key := ""
	if item, ok := rowAt(rows, idx); ok {
		key = item.schedule.ID
	}

	m.placeColumn(screenSchedules, key)
}

func (m model) schedulesView() string {
	return m.columnView(model.schedulesListView)
}

func (m model) schedulesHeader() string {
	return m.listHeader("Schedules")
}

func (m model) doesSchedulesFitDetails() bool {
	return m.doesListFitDetails(m.schedulesHeader(), model.scheduleHelp)
}

func (m model) schedulesListView() string {
	header := m.schedulesHeader()
	record, help := m.detailsView(), m.renderHelp(m.scheduleHelp(), header)
	below := record + m.messagesRegion(record, help, header) + help

	all := m.scheduleRows()
	if len(all) == 0 {
		return m.headedListView(header, []string{dimStyle.Render("  " + m.emptyListLine("no schedules yet"))}, 0, below)
	}

	rows := make([]string, 0, len(all))
	for i, item := range all {
		selected := i == m.schedules.cursor
		prefix := "  "
		if selected {
			prefix = m.rowCursorPrefix()
		}

		name := m.filter.highlight(paint.Panel, clipName(item.schedule.Name), m.nameStyle(selected, !item.schedule.Enabled))
		icon := paint.Panel.Style(scheduleTone(item.schedule.Enabled)).Render(scheduleIcon(item.schedule.Enabled))
		rows = append(rows, prefix+"  "+icon+" "+name+"  "+dimStyle.Render(item.schedule.Cron))
	}

	return m.headedListView(header, rows, m.schedules.cursor, below)
}

func (m *model) applySchedules(fresh []scheduleItem) {
	selected := ""
	if item, ok := m.selectedSchedule(); ok {
		selected = item.schedule.ID
	}

	m.schedules.items = fresh

	rows := m.scheduleRows()
	for i, item := range rows {
		if item.schedule.ID == selected {
			m.landScheduleCursor(rows, i)

			return
		}
	}

	m.landScheduleCursor(rows, clampCursor(m.schedules.cursor, len(rows)))
}
