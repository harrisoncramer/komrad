package ui

import (
	"fmt"
	"time"

	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
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
	m.schedules.cursor = clampCursor(m.schedules.cursor, len(m.scheduleRows()))
	m.stageSchedule()

	return m, nil
}

func (m model) scheduleRows() []scheduleItem {
	if m.filter.pattern == nil {
		return m.schedules.items
	}

	return xslice.Filter(m.schedules.items, func(item scheduleItem) bool {
		return m.filter.doesMatch(item.schedule.Name)
	})
}

func (m model) updateSchedules(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Filter.Open):
		return m.openFilter()
	case isBound(k, m.keys.Schedules.Quit):
		return m, m.hide()
	case m.filter.isStanding() && isBound(k, m.keys.Schedules.Back):
		return m.openFilter()
	case isBound(k, m.keys.Schedules.Back):
		return m.returnToAgents()
	case isBound(k, m.keys.Nav.Up):
		if next, ok := stepCursor(m.schedules.cursor, -1, len(m.scheduleRows())); ok {
			m.schedules.cursor = next
			m.stageSchedule()
		}
	case isBound(k, m.keys.Nav.Down):
		if next, ok := stepCursor(m.schedules.cursor, 1, len(m.scheduleRows())); ok {
			m.schedules.cursor = next
			m.stageSchedule()
		}
	case isBound(k, m.keys.Schedules.New):
		return m.newScheduleForm()
	case isBound(k, m.keys.Schedules.Edit):
		return m.editScheduleForm()
	case isBound(k, m.keys.Schedules.Toggle):
		return m.toggleSchedule()
	case isBound(k, m.keys.Schedules.Archive):
		return m.archiveScheduleConfirm()
	}

	return m, nil
}

func (m model) selectedSchedule() (scheduleItem, bool) {
	return rowAt(m.scheduleRows(), m.schedules.cursor)
}

// hoveredScheduleKey is the row the cursor is on, read before a filter narrows the list under it.
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

	m.schedules.cursor = placeCursor(found, m.schedules.cursor, len(rows))
	m.stageSchedule()
}

func (m model) schedulesDetails() details {
	item, ok := m.selectedSchedule()
	if !ok {
		return details{}
	}

	return details{
		id:     item.schedule.ID,
		kind:   "Schedule",
		title:  item.schedule.Name,
		fields: scheduleFields(item.schedule, m.workflowByID(item.schedule.WorkflowID)),
	}
}

func (m *model) stageSchedule() {
	item, ok := m.selectedSchedule()
	if !ok {
		m.stageDocument(noSchedulesMarkdown)

		return
	}

	m.stageDocument(scheduleMarkdown(item))
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
	return m.openRedrawnForm(
		formSchedule,
		func(m model) *huh.Form { return scheduleForm(values, m.listing.workflows, m.keys.Form) },
		func(m model) (tea.Model, tea.Cmd) {
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
	)
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

	form, confirmed := archiveScheduleForm(target.Name, m.keys.Form)

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

func (m model) schedulesView() string {
	below := m.detailsView() + "\n" + m.renderHelp(m.scheduleHelp())

	all := m.scheduleRows()
	if len(all) == 0 {
		return m.listView("Schedules", []string{dimStyle.Render("  no schedules yet")}, 0, below)
	}

	rows := make([]string, 0, len(all))
	for i, item := range all {
		selected := i == m.schedules.cursor
		prefix := "  "
		if selected {
			prefix = m.rowCursorStyle().Render("› ")
		}

		name := m.filter.highlight(item.schedule.Name, m.nameStyle(selected, false, !item.schedule.Enabled))
		icon := lipgloss.NewStyle().Foreground(scheduleColor(item.schedule.Enabled)).Render(scheduleIcon(item.schedule.Enabled))
		rows = append(rows, prefix+"  "+icon+" "+name+"  "+dimStyle.Render(item.schedule.Cron))
	}

	return m.listView("Schedules", rows, m.schedules.cursor, below)
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
			m.schedules.cursor = i

			return
		}
	}

	m.schedules.cursor = clampCursor(m.schedules.cursor, len(rows))
}
