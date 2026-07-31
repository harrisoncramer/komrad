package ui

import (
	"fmt"
	"time"

	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// scheduleState is the schedules screen, the listing it draws and which row is
// hovered.
type scheduleState struct {
	items  []scheduleItem
	cursor int
}

// scheduleItem is a stored schedule plus when it next fires. The next run is
// derived by the service from the cron expression rather than stored, so it
// arrives with the listing and is held beside the record the same way an
// agent's liveness is.
type scheduleItem struct {
	schedule schedulestore.Schedule
	nextRun  time.Time
}

// enterSchedules switches to the schedules view, both from the agents list and
// after one of the schedule forms closes. It is its own screen rather than a
// section of the agents tree, because a schedule is a record of future work with
// nothing running to stage beside it, so it shares neither the tree's shape nor
// its keys.
func (m model) enterSchedules() (tea.Model, tea.Cmd) {
	m.screen = screenSchedules
	m.schedules.cursor = clampCursor(m.schedules.cursor, len(m.schedules.items))
	m.stageSchedule()

	return m, nil
}

// updateSchedules drives the schedules list. Arrows move the selection and
// restage, and the per-row keys act on the hovered schedule.
func (m model) updateSchedules(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case bound(k, m.keys.Schedules.Quit):
		return m, m.hide()
	case bound(k, m.keys.Schedules.Back):
		return m.returnToAgents()
	case bound(k, m.keys.Nav.Up):
		if m.schedules.cursor > 0 {
			m.schedules.cursor--
			m.stageSchedule()
		}
	case bound(k, m.keys.Nav.Down):
		if m.schedules.cursor < len(m.schedules.items)-1 {
			m.schedules.cursor++
			m.stageSchedule()
		}
	case bound(k, m.keys.Schedules.New):
		return m.newScheduleForm()
	case bound(k, m.keys.Schedules.Edit):
		return m.editScheduleForm()
	case bound(k, m.keys.Schedules.Toggle):
		return m.toggleSchedule()
	case bound(k, m.keys.Schedules.Delete):
		return m.deleteScheduleConfirm()
	}

	return m, nil
}

// selectedSchedule returns the schedule under the cursor, or false when the
// list is empty.
func (m model) selectedSchedule() (scheduleItem, bool) {
	if m.schedules.cursor < 0 || m.schedules.cursor >= len(m.schedules.items) {
		return scheduleItem{}, false
	}

	return m.schedules.items[m.schedules.cursor], true
}

// stageSchedule shows the hovered schedule's record in the stage. Nothing runs
// for a schedule, so there is never a pane to swap in and the record is all
// there is to show.
func (m model) stageSchedule() {
	item, ok := m.selectedSchedule()
	if !ok {
		m.stage.Text(noSchedulesMarkdown)
		return
	}

	m.stage.Text(scheduleMarkdown(item, m.workflowByID(item.schedule.WorkflowID)))
}

// workflowByID finds a workflow in the list the panel already polls, nil when
// none holds that id, which means it was deleted.
func (m model) workflowByID(id string) *workflowstore.Workflow {
	for _, w := range m.tree.workflows {
		if w.ID == id {
			return &w
		}
	}

	return nil
}

// toggleSchedule pauses or resumes the hovered schedule. One key does both
// directions, so which way it went has to come from the row for the failure to
// name it.
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

// newScheduleForm opens the form to create a schedule, blank except for the
// workflow, which starts on the first in the list since one has to be chosen.
func (m model) newScheduleForm() (tea.Model, tea.Cmd) {
	if len(m.tree.workflows) == 0 {
		m.report("Could not create a schedule", errNoWorkflow)

		return m, nil
	}

	m.target.editSchedule = nil
	m.form, m.values.schedule = scheduleForm(schedulestore.Config{
		Enabled:    true,
		WorkflowID: m.tree.workflows[0].ID,
	}, m.tree.workflows, m.keys.Form)
	m.screen = screenScheduleForm

	return m, m.form.Init()
}

// editScheduleForm reopens the form for the hovered schedule, prefilled with
// its stored values. It refetches first, so the form edits what is stored
// rather than what the last listing happened to carry.
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
	}, m.tree.workflows, m.keys.Form)
	m.screen = screenScheduleForm

	return m, m.form.Init()
}

// deleteScheduleConfirm opens the delete prompt for the hovered schedule.
func (m model) deleteScheduleConfirm() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSchedule()
	if !ok {
		return m, nil
	}

	target := item.schedule
	m.target.deleteSchedule = &target
	m.form, m.values.confirmed = deleteScheduleForm(target.Name, m.keys.Form)
	m.screen = screenScheduleDelete

	return m, m.form.Init()
}

// updateScheduleForm drives the create and edit form, submitting on the
// configured submit key or on completion and aborting back to the list.
func (m model) updateScheduleForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && bound(key.String(), m.keys.Form.Submit) {
		return m.submitScheduleForm()
	}

	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		return m.submitScheduleForm()
	case huh.StateAborted:
		m.target.editSchedule = nil
		m.values.schedule = nil
		return m.enterSchedules()
	default:
		return m, cmd
	}
}

// submitScheduleForm applies the form, editing the target schedule or creating
// a new one, then returns to the list with it refreshed. A rejected expression
// comes back as a banner rather than silently doing nothing, since the list
// looking unchanged is exactly what a failed submit looks like otherwise.
func (m model) submitScheduleForm() (tea.Model, tea.Cmd) {
	// The form owns nothing the panel has not been handed a pointer to, so a submit
	// with no bound values is a submit for a form that was never opened.
	if m.values.schedule == nil {
		return m.enterSchedules()
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

	m.target.editSchedule = nil
	m.values.schedule = nil
	entered, refresh := m.enterSchedules()

	return entered, tea.Batch(write, refresh)
}

// updateScheduleDelete drives the delete prompt. Confirming removes the
// schedule, and both outcomes return to the list.
func (m model) updateScheduleDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		var write tea.Cmd
		if target := m.target.deleteSchedule; target != nil && m.wasConfirmed() {
			write = m.writeCmd(
				fmt.Sprintf("Could not delete schedule %q", target.Name),
				func(a api) error { return a.deleteSchedule(target.ID) },
			)
		}
		m.target.deleteSchedule = nil
		m.values.confirmed = nil
		entered, refresh := m.enterSchedules()

		return entered, tea.Batch(write, refresh)
	case huh.StateAborted:
		m.target.deleteSchedule = nil
		m.values.confirmed = nil

		return m.enterSchedules()
	default:
		return m, cmd
	}
}

// schedulesView renders the list. Each row is a schedule with its cron
// expression and when it next fires, since those two are what a person checks a
// schedule for.
func (m model) schedulesView() string {
	header := headerStyle.Render("Schedules") + "\n\n"
	hints := "\n" + m.scheduleHints()

	if len(m.schedules.items) == 0 {
		return header + dimStyle.Render("  no schedules yet") + "\n" + hints
	}

	rows := make([]string, 0, len(m.schedules.items))
	for i, item := range m.schedules.items {
		selected := i == m.schedules.cursor
		prefix := "  "
		if selected {
			prefix = cursorStyle.Render("› ")
		}

		name := item.schedule.Name
		if !item.schedule.Enabled {
			name = dimStyle.Render(name)
		}
		if selected {
			name = cursorStyle.Render(item.schedule.Name)
		}

		icon := lipgloss.NewStyle().Foreground(scheduleColor(item.schedule.Enabled)).Render(scheduleIcon(item.schedule.Enabled))
		rows = append(rows, prefix+"  "+icon+" "+name+"  "+dimStyle.Render(item.schedule.Cron))
	}

	return header + renderList(rows, m.schedules.cursor, m.listLines(header, hints)) + hints
}

// applySchedules takes a freshly-listed set as the on-screen schedules. It keeps the
// cursor on the schedule it was on rather than on an index that now means a
// different row, since a schedule deleted from another panel shifts everything below
// it up.
func (m *model) applySchedules(fresh []scheduleItem) {
	selected := ""
	if item, ok := m.selectedSchedule(); ok {
		selected = item.schedule.ID
	}

	m.schedules.items = fresh

	for i, item := range fresh {
		if item.schedule.ID == selected {
			m.schedules.cursor = i

			return
		}
	}

	m.schedules.cursor = clampCursor(m.schedules.cursor, len(fresh))
}
