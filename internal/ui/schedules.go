package ui

import (
	"fmt"
	"time"

	"weave/config"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	"weave/internal/view"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// schedulesScreen is the schedules screen, holding its own cursor behind the panel's one door.
type schedulesScreen struct {
	cursor int
	rows   []scheduleItem
}

var (
	_ detailed = (*schedulesScreen)(nil)
	_ filtered = (*schedulesScreen)(nil)
	_ columned = (*schedulesScreen)(nil)
)

type scheduleItem struct {
	schedule panel.Schedule
	nextRun  time.Time
}

// enter lands the cursor once the root has put the screen up.
func (s *schedulesScreen) enter(h host, p panelShared) {
	rows := s.listRows(p)
	s.land(h, rows, clampCursor(s.cursor, len(rows)))
}

// move steps the cursor, reporting a step it refused at either end.
func (s *schedulesScreen) move(h host, p panelShared, delta int) bool {
	rows := s.listRows(p)
	next, ok := stepCursor(s.cursor, delta, len(rows))
	if !ok {
		return false
	}

	s.land(h, rows, next)

	return true
}

func (s *schedulesScreen) jump(h host, p panelShared, idx int) {
	rows := s.listRows(p)
	if next, ok := landCursor(idx, len(rows)); ok {
		s.land(h, rows, next)
	}
}

// listRows is the list as drawn, or as it would be drawn when nothing has cached it.
func (s *schedulesScreen) listRows(p panelShared) []scheduleItem {
	if s.rows != nil {
		return s.rows
	}

	return s.buildRows(p)
}

func (s *schedulesScreen) buildRows(p panelShared) []scheduleItem {
	return narrowed(p.chrome.filter, narrowedParams[scheduleItem]{
		rows:  p.listing.schedules,
		depth: func(scheduleItem) int { return 0 },
		name:  func(item scheduleItem) string { return item.schedule.Name },
	})
}

func (s *schedulesScreen) selected(p panelShared) (scheduleItem, bool) {
	return rowAt(s.listRows(p), s.cursor)
}

// hovered is the row the cursor is on, read before a filter narrows the list under it.
func (s *schedulesScreen) hovered(p panelShared) string {
	item, ok := s.selected(p)
	if !ok {
		return ""
	}

	return item.schedule.ID
}

// place puts the cursor back on a row by key, or as near to where it was as the list allows.
func (s *schedulesScreen) place(h host, p panelShared, hovered string) {
	rows := s.listRows(p)

	found := -1
	for i, item := range rows {
		if item.schedule.ID == hovered {
			found = i
		}
	}

	s.land(h, rows, placeCursor(found, s.cursor, len(rows)))
}

// land puts the cursor on a row and names that row to the column.
func (s *schedulesScreen) land(h host, rows []scheduleItem, idx int) {
	s.cursor = idx

	key := ""
	if item, ok := rowAt(rows, idx); ok {
		key = item.schedule.ID
	}

	h.placeColumn(screenSchedules, key)
}

// record is the hovered schedule as the details under the list draw it.
func (s *schedulesScreen) record(p panelShared) details {
	item, ok := s.selected(p)
	if !ok {
		return details{}
	}

	return details{
		id:     item.schedule.ID,
		fields: scheduleFields(item.schedule, workflowByID(p.listing.workflows, item.schedule.WorkflowID)),
	}
}

// doc is the markdown drawn beside the list: the notice for an empty list, or the schedule the
// cursor is on.
func (s *schedulesScreen) doc(p panelShared) string {
	item, ok := s.selected(p)
	if !ok {
		return noSchedulesMarkdown
	}

	return scheduleMarkdown(item)
}

func workflowByID(workflows []panel.Workflow, id string) *panel.Workflow {
	for _, w := range workflows {
		if w.ID == id {
			return &w
		}
	}

	return nil
}

func (s *schedulesScreen) toggle(h host, p panelShared) tea.Cmd {
	item, ok := s.selected(p)
	if !ok {
		return nil
	}

	failure := fmt.Sprintf("Could not pause schedule %q", item.schedule.Name)
	if !item.schedule.Enabled {
		failure = fmt.Sprintf("Could not resume schedule %q", item.schedule.Name)
	}

	return h.writeCmd(failure, func(a api) error {
		return a.setScheduleEnabled(item.schedule.ID, !item.schedule.Enabled)
	})
}

func (s *schedulesScreen) newForm(h host, p panelShared) tea.Cmd {
	if len(p.listing.workflows) == 0 {
		h.report("Could not create a schedule", errNoWorkflow)

		return nil
	}

	values := panel.ScheduleConfig{
		Enabled:    true,
		WorkflowID: p.listing.workflows[0].ID,
	}

	return s.openForm(h, &values, nil)
}

// openForm puts up the schedule form, creating a schedule when target is nil and saving over the
// one it names otherwise.
func (s *schedulesScreen) openForm(h host, values *panel.ScheduleConfig, target *panel.Schedule) tea.Cmd {
	return h.openForm(redrawnForm(openRedrawnFormParams{
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
	}))
}

func (s *schedulesScreen) editForm(h host, p panelShared) tea.Cmd {
	item, ok := s.selected(p)
	if !ok {
		return nil
	}

	target := item.schedule
	if fresh, err := h.client().getSchedule(target.ID); err == nil && fresh != nil {
		target = *fresh
	}

	values := schedulePrefill(target)

	return s.openForm(h, &values, &target)
}

// schedulePrefill is the schedule form as the stored schedule leaves it.
func schedulePrefill(schedule panel.Schedule) panel.ScheduleConfig {
	return panel.ScheduleConfig{
		Name:       schedule.Name,
		Cron:       schedule.Cron,
		Timezone:   schedule.Timezone,
		Enabled:    schedule.Enabled,
		HideAgents: schedule.HideAgents,
		Prompt:     schedule.Prompt,
		WorkflowID: schedule.WorkflowID,
	}
}

func (s *schedulesScreen) archiveConfirm(h host, p panelShared) tea.Cmd {
	item, ok := s.selected(p)
	if !ok {
		return nil
	}

	target := item.schedule

	form, confirmed := archiveScheduleForm(target.Name, formKeys())

	return h.openForm(formState{
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

// applySchedules takes a fresh listing, keeping the cursor on the schedule it was on.
func (m *model) applySchedules(fresh []scheduleItem) {
	selected := m.schedules.hovered(m.shared())
	m.listing.schedules = fresh
	m.schedules.place(m, m.shared(), selected)
}

func (s *schedulesScreen) update(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	return onKey(m, msg, func(m model, key tea.KeyMsg) (tea.Model, tea.Cmd) {
		return schedulesActions().dispatch(m, key.String())
	})
}

// view draws the list beside its record.
func (s *schedulesScreen) view(m model) string {
	m.schedules.rows = s.buildRows(m.shared())

	return m.columnView(func(held model) string { return held.schedules.listView(held) })
}

func (s *schedulesScreen) header(m model) string {
	return m.listHeader(screenTitle(view.GlyphSchedule, "Schedules"))
}

func (s *schedulesScreen) listView(m model) string {
	header := s.header(m)
	record, help := m.detailsView(), m.renderHelp(m.scheduleHelp(), header)
	below := record + m.messagesRegion(record, help, header) + help

	all := s.listRows(m.shared())
	if len(all) == 0 {
		return m.headedListView(header, []string{dimStyle.Render("  " + m.emptyListLine("no schedules yet"))}, 0, below)
	}

	rows := make([]string, 0, len(all))
	for i, item := range all {
		selected := i == s.cursor
		prefix := "  "
		if selected {
			prefix = m.rowCursorPrefix()
		}

		name := m.filter.highlight(paint.Panel, clipName(item.schedule.Name), m.nameStyle(selected && m.caretOf(sectionList) != view.CaretAbsent, !item.schedule.Enabled))
		icon := paint.Panel.Style(scheduleTone(item.schedule.Enabled)).Render(scheduleIcon(item.schedule.Enabled))
		rows = append(rows, prefix+"  "+icon+" "+name+"  "+dimStyle.Render(item.schedule.Cron))
	}

	return m.headedListView(header, rows, s.cursor, below)
}

func (s *schedulesScreen) needs() screenNeeds {
	return screenNeeds{fromListing: true}
}

// open puts the screen up and lands its cursor.
func (s *schedulesScreen) open(m model, _ screenTarget) (tea.Model, tea.Cmd) {
	m.enterScreen(screenSchedules)
	m.schedules.enter(&m, m.shared())

	return m, nil
}

func (s *schedulesScreen) takesEveryKey(model) bool {
	return false
}

func (s *schedulesScreen) details(m model) details {
	return s.record(m.shared())
}

func (s *schedulesScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Schedules.Quit
}

func (s *schedulesScreen) detailsFit(m model) bool {
	return m.doesListFitDetails(s.header(m), model.scheduleHelp)
}

func (s *schedulesScreen) detailsRoom(m model) int {
	return m.listDetailsRoom(s.header(m), model.scheduleHelp)
}

func (s *schedulesScreen) hoveredKey(m model) string {
	return s.hovered(m.shared())
}

func (s *schedulesScreen) placeAt(m *model, key string) {
	m.schedules.place(m, m.shared(), key)
}

func (s *schedulesScreen) filterRows(m model) []filterRow {
	return xslice.Map(s.listRows(m.shared()), func(item scheduleItem) filterRow {
		return filterRow{key: item.schedule.ID, name: item.schedule.Name}
	})
}

// column is the hovered schedule's own record drawn beside the list.
func (s *schedulesScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   func(m model) string { return m.schedules.doc(m.shared()) },
	}
}
