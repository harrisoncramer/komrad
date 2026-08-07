package ui

import (
	"fmt"
	"time"

	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type scheduleState struct {
	items  []scheduleItem
	cursor int
}

// scheduleItem is a stored schedule plus when it next fires. The next run is derived by
// the service from the cron expression rather than stored, so it arrives with the listing
// the way an agent's liveness does.
type scheduleItem struct {
	schedule schedulestore.Schedule
	nextRun  time.Time
}

// enterSchedules is its own screen rather than part of the agents tree, since a schedule is
// a record of future work with nothing running to stage beside it.
func (m model) enterSchedules() (tea.Model, tea.Cmd) {
	m.enterScreen(screenSchedules)
	m.closeRecord()
	m.schedules.cursor = clampCursor(m.schedules.cursor, len(m.scheduleRows()))
	m.stageSchedule()

	return m, nil
}

// scheduleRows is the listing less whatever the filter holds back; the cursor indexes
// these rather than the listing, so a key acts on the row it is on.
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

	if cmd, ok := m.tryCopyRecordValue(k); ok {
		return m, cmd
	}

	switch {
	case isBound(k, m.keys.Filter.Open):
		return m.openFilter()
	case isBound(k, m.keys.Schedules.Quit):
		return m, m.hide()
	case m.filter.standing() && isBound(k, m.keys.Schedules.Back):
		return m.clearFilter()
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

// stageSchedule shows the hovered schedule's record in the stage; nothing runs for a
// schedule, so there is never a pane to swap in.
func (m *model) stageSchedule() {
	item, ok := m.selectedSchedule()
	if !ok {
		m.stagePlaceholder(noSchedulesMarkdown)

		return
	}

	m.stageRecord(scheduleMarkdown(item, m.workflowByID(item.schedule.WorkflowID)), layout.RegionStage)
}

// workflowByID returns nil when none holds that id, which means it was archived.
func (m model) workflowByID(id string) *workflowstore.Workflow {
	for _, w := range m.listing.workflows {
		if w.ID == id {
			return &w
		}
	}

	return nil
}

// toggleSchedule pauses or resumes the hovered schedule; one key does both directions, so
// the failure message comes off the row.
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

// newScheduleForm starts the workflow field on the first in the list, since one has to
// be chosen.
func (m model) newScheduleForm() (tea.Model, tea.Cmd) {
	if len(m.listing.workflows) == 0 {
		m.report("Could not create a schedule", errNoWorkflow)

		return m, nil
	}

	m.target.editSchedule = nil
	m.form, m.values.schedule = scheduleForm(schedulestore.Config{
		Enabled:    true,
		WorkflowID: m.listing.workflows[0].ID,
	}, m.listing.workflows, m.keys.Form)
	m.screen = screenScheduleForm

	return m, m.form.Init()
}

// editScheduleForm refetches first, so the form edits what is stored rather than what the
// last listing carried.
func (m model) editScheduleForm() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSchedule()
	if !ok {
		return m, nil
	}

	target := item.schedule
	if fresh, err := m.api.getSchedule(target.ID); err == nil && fresh != nil {
		target = *fresh
	}

	m.target.editSchedule = &target
	m.form, m.values.schedule = scheduleForm(schedulestore.Config{
		Name:       target.Name,
		Cron:       target.Cron,
		Timezone:   target.Timezone,
		Enabled:    target.Enabled,
		HideAgents: target.HideAgents,
		Prompt:     target.Prompt,
		WorkflowID: target.WorkflowID,
	}, m.listing.workflows, m.keys.Form)
	m.screen = screenScheduleForm

	return m, m.form.Init()
}

// archiveScheduleConfirm opens the archive prompt for the hovered schedule.
func (m model) archiveScheduleConfirm() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSchedule()
	if !ok {
		return m, nil
	}

	target := item.schedule
	m.target.archiveSchedule = &target
	m.form, m.values.confirmed = archiveScheduleForm(target.Name, m.keys.Form)
	m.screen = screenScheduleArchive

	return m, m.form.Init()
}

func (m model) updateScheduleForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateSubmitForm(msg, model.submitScheduleForm)
}

func (m model) submitScheduleForm() (tea.Model, tea.Cmd) {
	// The form writes through pointers the panel holds, so a submit with no bound values
	// is a submit for a form that was never opened.
	if m.values.schedule == nil {
		return m.closeForm()
	}

	cfg := *m.values.schedule

	var write tea.Cmd
	if target := m.target.editSchedule; target != nil {
		write = m.writeCmd(
			fmt.Sprintf("Could not save schedule %q", target.Name),
			func(a api) error { return a.updateSchedule(target.ID, cfg) },
		)
	} else {
		write = m.writeCmd("Could not create the schedule", func(a api) error { return a.createSchedule(cfg) })
	}

	return m, tea.Batch(write, m.holdForm())
}

func (m model) updateScheduleArchive(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateConfirmForm(msg, confirmFormActions{
		write: func(m model) tea.Cmd {
			target := m.target.archiveSchedule
			if target == nil || !m.wasConfirmed() {
				return nil
			}

			return m.writeCmd(
				fmt.Sprintf("Could not archive schedule %q", target.Name),
				func(a api) error { return a.archiveSchedule(target.ID) },
			)
		},
		clear: func(m model) model {
			m.target.archiveSchedule = nil
			m.values.confirmed = nil

			return m
		},
		returnTo: func(m model) (tea.Model, tea.Cmd) {
			return m.enterSchedules()
		},
	})
}

func (m model) schedulesView() string {
	help := "\n" + m.renderHelp(m.scheduleHelp())

	all := m.scheduleRows()
	if len(all) == 0 {
		return m.listView("Schedules", []string{dimStyle.Render("  no schedules yet")}, 0, help)
	}

	rows := make([]string, 0, len(all))
	for i, item := range all {
		selected := i == m.schedules.cursor
		prefix := "  "
		if selected {
			prefix = cursorStyle.Render("› ")
		}

		name := m.filter.highlight(item.schedule.Name, nameStyle(selected, !item.schedule.Enabled))
		icon := lipgloss.NewStyle().Foreground(scheduleColor(item.schedule.Enabled)).Render(scheduleIcon(item.schedule.Enabled))
		rows = append(rows, prefix+"  "+icon+" "+name+"  "+dimStyle.Render(item.schedule.Cron))
	}

	return m.listView("Schedules", rows, m.schedules.cursor, help)
}

// applySchedules keeps the cursor on the schedule it was on, since a schedule archived from
// another panel shifts everything below it up.
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
