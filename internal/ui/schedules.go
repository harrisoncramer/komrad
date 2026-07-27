package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/client"
	"claude-agents/lib/dates"
	schedulev1 "claude-agents/proto/gen/schedule/v1"

	"connectrpc.com/connect"

	schedulestore "claude-agents/internal/store/schedule"
	workflowstore "claude-agents/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

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
// its actions.
func (m model) enterSchedules() (tea.Model, tea.Cmd) {
	m.screen = screenSchedules
	m.scheduleCursor = clampCursor(m.scheduleCursor, len(m.schedules))
	m.stageSchedule()

	return m, nil
}

// updateSchedules drives the schedules list. Arrows move the selection and
// restage, and the action keys act on the hovered schedule.
func (m model) updateSchedules(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case bound(k, m.keys.Schedules.Quit):
		return m, m.hide()
	case bound(k, m.keys.Schedules.Back):
		return m.returnToAgents()
	case bound(k, m.keys.Nav.Up):
		if m.scheduleCursor > 0 {
			m.scheduleCursor--
			m.stageSchedule()
		}
	case bound(k, m.keys.Nav.Down):
		if m.scheduleCursor < len(m.schedules)-1 {
			m.scheduleCursor++
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
	if m.scheduleCursor < 0 || m.scheduleCursor >= len(m.schedules) {
		return scheduleItem{}, false
	}

	return m.schedules[m.scheduleCursor], true
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
	for _, w := range m.workflows {
		if w.ID == id {
			return &w
		}
	}

	return nil
}

// toggleSchedule pauses or resumes the hovered schedule.
func (m model) toggleSchedule() (tea.Model, tea.Cmd) {
	item, ok := m.selectedSchedule()
	if !ok {
		return m, nil
	}

	m.report(
		fmt.Sprintf("Could not pause schedule %q", item.schedule.Name),
		m.setScheduleEnabled(item.schedule.ID, !item.schedule.Enabled),
	)
	m.reloadSchedules()

	return m.enterSchedules()
}

// newScheduleForm opens the form to create a schedule, blank except for the
// workflow, which starts on the first in the list since one has to be chosen.
func (m model) newScheduleForm() (tea.Model, tea.Cmd) {
	if len(m.workflows) == 0 {
		m.failAction("Could not create a schedule", errNoWorkflow)

		return m, nil
	}

	m.scheduleEditTarget = nil
	m.form = scheduleForm(schedulestore.Config{
		Enabled:    true,
		WorkflowID: m.workflows[0].ID,
	}, m.workflows, m.keys.Form)
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
	if fresh, err := m.getSchedule(target.ID); err == nil && fresh != nil {
		target = *fresh
	}

	m.scheduleEditTarget = &target
	m.form = scheduleForm(schedulestore.Config{
		Name:       target.Name,
		Cron:       target.Cron,
		Timezone:   target.Timezone,
		Enabled:    target.Enabled,
		Prompt:     target.Prompt,
		WorkflowID: target.WorkflowID,
	}, m.workflows, m.keys.Form)
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
	m.scheduleDeleteTarget = &target
	m.form = deleteScheduleForm(target.Name, m.keys.Form)
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
		m.scheduleEditTarget = nil
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
	cfg := schedulestore.Config{
		Name:       m.form.GetString("name"),
		Cron:       m.form.GetString("cron"),
		Timezone:   m.form.GetString("timezone"),
		Enabled:    m.form.GetBool("enabled"),
		Prompt:     m.form.GetString("prompt"),
		WorkflowID: m.form.GetString("workflow"),
	}

	if target := m.scheduleEditTarget; target != nil {
		m.report(fmt.Sprintf("Could not save schedule %q", target.Name), m.updateSchedule(target.ID, cfg))
	} else {
		m.report("Could not create the schedule", m.createSchedule(cfg))
	}

	m.scheduleEditTarget = nil
	m.reloadSchedules()

	return m.enterSchedules()
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
		if target := m.scheduleDeleteTarget; target != nil && m.form.GetBool("confirm") {
			m.report(
				fmt.Sprintf("Could not delete schedule %q", target.Name),
				m.deleteSchedule(target.ID),
			)
		}
		m.scheduleDeleteTarget = nil
		m.reloadSchedules()

		return m.enterSchedules()
	case huh.StateAborted:
		m.scheduleDeleteTarget = nil

		return m.enterSchedules()
	default:
		return m, cmd
	}
}

// schedulesView renders the list. Each row is a schedule with its cron
// expression and when it next fires, since those two are what a person checks a
// schedule for.
func (m model) schedulesView() string {
	var list strings.Builder
	list.WriteString(headerStyle.Render("Schedules") + "\n\n")

	if len(m.schedules) == 0 {
		list.WriteString(dimStyle.Render("  no schedules yet") + "\n")
	}

	for i, item := range m.schedules {
		selected := i == m.scheduleCursor
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
		list.WriteString(prefix + "  " + icon + " " + name + "  " + dimStyle.Render(item.schedule.Cron) + "\n")
	}

	list.WriteString("\n" + m.scheduleHints())

	return list.String()
}

// scheduleHints renders the key hints for the schedules screen, in the same
// two-column shape the agents view uses.
func (m model) scheduleHints() string {
	toggle := "pause"
	if item, ok := m.selectedSchedule(); ok && !item.schedule.Enabled {
		toggle = "resume"
	}

	return renderHints([]hint{
		{hintKeys(m.keys.Schedules.New), "new schedule"},
		{hintKeys(m.keys.Schedules.Edit), "edit"},
		{hintKeys(m.keys.Schedules.Toggle), toggle},
		{hintKeys(m.keys.Schedules.Delete), "delete"},
		{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		{hintKeys(m.keys.Schedules.Back), "back"},
	})
}

// scheduleIcon marks whether a schedule is armed. A paused one keeps its row,
// so the list has to say which of the two it is at a glance.
func scheduleIcon(enabled bool) string {
	if enabled {
		return "◆"
	}

	return "◇"
}

func scheduleColor(enabled bool) lipgloss.Color {
	if enabled {
		return lipgloss.Color("#98BB6C")
	}

	return lipgloss.Color("#727169")
}

// noSchedulesMarkdown is what the stage shows with an empty list, so the pane
// beside it says what the screen is for rather than sitting blank.
const noSchedulesMarkdown = "# Schedules\n\n" +
	"A schedule launches an agent on a cron expression.\n\n" +
	"Press the new key to write one.\n"

// scheduleMarkdown renders a schedule's record for the stage.
func scheduleMarkdown(item scheduleItem, workflow *workflowstore.Workflow) string {
	sched := item.schedule

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", sched.Name)
	if sched.Enabled {
		fmt.Fprintf(&b, "_next run %s_\n\n", orDash(dates.Friendly(item.nextRun)))
	} else {
		b.WriteString("_paused — will not run_\n\n")
	}

	if strings.TrimSpace(sched.Prompt) != "" {
		b.WriteString(sched.Prompt + "\n\n")
	} else {
		b.WriteString("_no prompt set_\n\n")
	}

	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- cron: %s\n", orDash(sched.Cron))
	fmt.Fprintf(&b, "- timezone: %s\n", orDash(sched.Timezone))

	// The directory and flags belong to the workflow and are read as the schedule
	// fires, so they are shown under the workflow that supplies them rather than as
	// settings of the schedule itself.
	if workflow == nil {
		b.WriteString("- workflow: — (deleted)\n")

		return b.String()
	}

	fmt.Fprintf(&b, "- workflow: %s\n", workflow.Name)
	fmt.Fprintf(&b, "- directory: %s\n", orDash(workflow.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(workflow.Flags))

	return b.String()
}

// scheduleForm collects everything a schedule holds. Every field prefills, so
// it serves both creating one and editing one in place.
//
// There is no directory or flags field: those come off the workflow when the
// schedule fires, which is why picking the workflow is picking where and how the
// agent runs.
func scheduleForm(cfg schedulestore.Config, workflows []workflowstore.Workflow, keys appconfig.FormKeys) *huh.Form {
	name, cron, timezone := cfg.Name, cfg.Cron, cfg.Timezone
	prompt, workflow := cfg.Prompt, cfg.WorkflowID
	enabled := cfg.Enabled

	options := make([]huh.Option[string], 0, len(workflows))
	for _, w := range workflows {
		options = append(options, huh.NewOption(w.Name+"  "+orDash(w.Cwd), w.ID))
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title("Name").Description("also names the agents it launches").Value(&name),
			huh.NewInput().Key("cron").Title("Cron").Placeholder("0 9 * * 1-5").Description("minute hour day month weekday").Value(&cron),
			huh.NewInput().Key("timezone").Title("Timezone").Placeholder("America/New_York").Description("blank = this machine's zone").Value(&timezone),
			huh.NewText().Key("prompt").Title("Prompt").Description("what each launched agent is asked to do").Value(&prompt),
			huh.NewSelect[string]().Key("workflow").Title("Workflow").Description("supplies the directory, flags, and context").Options(options...).Value(&workflow),
			huh.NewConfirm().Key("enabled").Title("Armed?").Affirmative("Armed").Negative("Paused").Value(&enabled),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))
}

// deleteScheduleForm confirms a delete. The agents the schedule already
// launched are not touched either way, which the prompt says so the choice is
// not read as bigger than it is.
func deleteScheduleForm(name string, keys appconfig.FormKeys) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("confirm").
				Title(fmt.Sprintf("Delete schedule %q?", name)).
				Description("The agents it already launched are kept.").
				Affirmative("Delete").
				Negative("Cancel"),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))
}

// The schedule screens act through the service like everything else. Each call
// carries its own short deadline so a wedged server costs a keypress rather
// than the panel.

// listSchedules fetches every schedule and renders them into the items the list
// is written against.
func (m model) listSchedules() ([]scheduleItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
	defer cancel()

	listed, err := client.ListAllSchedules(ctx, m.api.schedules)
	if err != nil {
		return nil, err
	}

	return scheduleItemsFrom(listed), nil
}

// reloadSchedules refreshes the list after an action, clamping the cursor into
// the new length.
func (m *model) reloadSchedules() {
	schedules, err := m.listSchedules()
	if err != nil {
		// The list already on screen is kept. Blanking it would say every schedule
		// had been deleted, which is not what a list that failed to answer means.
		m.failAction("Could not list schedules", err)

		return
	}

	m.schedules = schedules
	m.scheduleCursor = clampCursor(m.scheduleCursor, len(schedules))
}

// getSchedule refetches one schedule, so an edit form opens on what is stored
// rather than on the row the listing happened to carry.
func (m model) getSchedule(id string) (*schedulestore.Schedule, error) {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	resp, err := m.api.schedules.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule %q: %w", id, err)
	}

	stored := scheduleFromProto(resp.Msg.GetSchedule())

	return &stored, nil
}

// createSchedule records a new schedule.
func (m model) createSchedule(cfg schedulestore.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := m.api.schedules.CreateSchedule(ctx, connect.NewRequest(&schedulev1.CreateScheduleRequest{
		Name:       strings.TrimSpace(cfg.Name),
		Cron:       cfg.Cron,
		Timezone:   cfg.Timezone,
		Enabled:    cfg.Enabled,
		Prompt:     cfg.Prompt,
		WorkflowId: cfg.WorkflowID,
	}))

	return err
}

// updateSchedule writes every field back, which is what the service expects. It
// replaces rather than patches, so the caller sends the whole schedule.
func (m model) updateSchedule(id string, cfg schedulestore.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := m.api.schedules.UpdateSchedule(ctx, connect.NewRequest(&schedulev1.UpdateScheduleRequest{
		Id:         id,
		Name:       strings.TrimSpace(cfg.Name),
		Cron:       cfg.Cron,
		Timezone:   cfg.Timezone,
		Enabled:    cfg.Enabled,
		Prompt:     cfg.Prompt,
		WorkflowId: cfg.WorkflowID,
	}))

	return err
}

// setScheduleEnabled pauses or resumes a schedule without touching the rest of
// its record.
func (m model) setScheduleEnabled(id string, enabled bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := m.api.schedules.SetScheduleEnabled(ctx, connect.NewRequest(&schedulev1.SetScheduleEnabledRequest{
		Id:      id,
		Enabled: enabled,
	}))

	return err
}

// deleteSchedule removes a schedule. The agents it launched are left alone.
func (m model) deleteSchedule(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := m.api.schedules.DeleteSchedule(ctx, connect.NewRequest(&schedulev1.DeleteScheduleRequest{Id: id}))

	return err
}

// scheduleItemsFrom maps listed schedules into the items the list is written
// against, carrying the next run the service derived.
func scheduleItemsFrom(listed []*schedulev1.Schedule) []scheduleItem {
	items := make([]scheduleItem, 0, len(listed))
	for _, s := range listed {
		item := scheduleItem{schedule: scheduleFromProto(s)}
		if next := s.GetNextRunAt(); next != nil {
			item.nextRun = next.AsTime().Local()
		}
		items = append(items, item)
	}

	return items
}

// scheduleFromProto renders a listed schedule into the store type the screen is
// written against.
func scheduleFromProto(s *schedulev1.Schedule) schedulestore.Schedule {
	return schedulestore.Schedule{
		ID:         s.GetId(),
		Name:       s.GetName(),
		Cron:       s.GetCron(),
		Timezone:   s.GetTimezone(),
		Enabled:    s.GetEnabled(),
		Prompt:     s.GetPrompt(),
		WorkflowID: s.GetWorkflowId(),
	}
}

// applySchedules merges a freshly-listed set into the on-screen schedules,
// keeping the cursor on the schedule it was on rather than on an index that now
// means a different row. The listing sorts by recency, so an edit anywhere
// would otherwise move rows under the cursor.
func (m *model) applySchedules(fresh []scheduleItem) {
	selected := ""
	if item, ok := m.selectedSchedule(); ok {
		selected = item.schedule.ID
	}

	m.schedules = fresh

	for i, item := range fresh {
		if item.schedule.ID == selected {
			m.scheduleCursor = i

			return
		}
	}

	m.scheduleCursor = clampCursor(m.scheduleCursor, len(fresh))
}
