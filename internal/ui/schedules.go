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

// scheduleState is the schedules screen, the listing it draws and which row is
// hovered.
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

// enterSchedules switches to the schedules view, from the agents list and after one of the
// schedule forms closes. It is its own screen rather than part of the agents tree, since a
// schedule is a record of future work with nothing running to stage beside it.
func (m model) enterSchedules() (tea.Model, tea.Cmd) {
	m.enterScreen(screenSchedules)
	m.schedules.cursor = clampCursor(m.schedules.cursor, len(m.scheduleRows()))
	m.stageSchedule()

	return m, nil
}

// scheduleRows are the schedules the list draws, the listing less whatever the filter holds
// back. The cursor indexes these rather than the listing, so a key acts on the row it is on.
func (m model) scheduleRows() []scheduleItem {
	if m.filter.pattern == nil {
		return m.schedules.items
	}

	return xslice.Filter(m.schedules.items, func(item scheduleItem) bool {
		return m.filter.doesMatch(item.schedule.Name)
	})
}

// updateSchedules drives the schedules list. Arrows move the selection and
// restage, and the per-row keys act on the hovered schedule.
func (m model) updateSchedules(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

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
		if m.schedules.cursor > 0 {
			m.schedules.cursor--
			m.stageSchedule()
		}
	case isBound(k, m.keys.Nav.Down):
		if m.schedules.cursor < len(m.scheduleRows())-1 {
			m.schedules.cursor++
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

// selectedSchedule returns the schedule under the cursor, or false when the
// list is empty.
func (m model) selectedSchedule() (scheduleItem, bool) {
	rows := m.scheduleRows()
	if m.schedules.cursor < 0 || m.schedules.cursor >= len(rows) {
		return scheduleItem{}, false
	}

	return rows[m.schedules.cursor], true
}

// stageSchedule shows the hovered schedule's record in the stage. Nothing runs for a
// schedule, so there is never a pane to swap in.
func (m model) stageSchedule() {
	item, ok := m.selectedSchedule()
	if !ok {
		m.stage.Text(noSchedulesMarkdown)
		return
	}

	m.stage.Text(scheduleMarkdown(item, m.workflowByID(item.schedule.WorkflowID)))
}

// workflowByID finds a workflow in the list the panel already polls, nil when none holds
// that id, which means it was archived.
func (m model) workflowByID(id string) *workflowstore.Workflow {
	for _, w := range m.tree.workflows {
		if w.ID == id {
			return &w
		}
	}

	return nil
}

// toggleSchedule pauses or resumes the hovered schedule. One key does both directions, so
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

// newScheduleForm opens the form to create a schedule, blank except for the workflow,
// which starts on the first in the list since one has to be chosen.
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

// editScheduleForm reopens the form for the hovered schedule. It refetches first, so the
// form edits what is stored rather than what the last listing carried.
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

// updateScheduleForm drives the create and edit form, submitting on the configured key or
// on completion and aborting back to the list.
func (m model) updateScheduleForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && isBound(key.String(), m.keys.Form.Submit) {
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
		return m.closeForm()
	default:
		return m, cmd
	}
}

// submitScheduleForm applies the form, editing the target schedule or creating one. A
// rejected cron expression comes back on the form itself, which is where it is fixed.
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

// updateScheduleArchive drives the archive prompt. Confirming puts the schedule
// away, and both outcomes return to the list.
func (m model) updateScheduleArchive(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		var write tea.Cmd
		if target := m.target.archiveSchedule; target != nil && m.wasConfirmed() {
			write = m.writeCmd(
				fmt.Sprintf("Could not archive schedule %q", target.Name),
				func(a api) error { return a.archiveSchedule(target.ID) },
			)
		}
		m.target.archiveSchedule = nil
		m.values.confirmed = nil
		entered, refresh := m.enterSchedules()

		return entered, tea.Batch(write, refresh)
	case huh.StateAborted:
		m.target.archiveSchedule = nil
		m.values.confirmed = nil

		return m.enterSchedules()
	default:
		return m, cmd
	}
}

// schedulesView renders the list, each row a schedule with its cron expression and when it
// next fires, which is what a person checks a schedule for.
func (m model) schedulesView() string {
	header := headerStyle.Render("Schedules") + "\n\n" + m.filterView()
	help := "\n" + m.renderHelp(m.scheduleHelp())

	all := m.scheduleRows()
	if len(all) == 0 {
		return header + dimStyle.Render("  no schedules yet") + "\n" + help
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

	return header + renderList(rows, m.schedules.cursor, m.listLines(header, help)) + help
}

// applySchedules takes a freshly-listed set as the on-screen schedules, keeping the cursor
// on the schedule it was on, since a schedule archived from another panel shifts everything
// below it up.
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
