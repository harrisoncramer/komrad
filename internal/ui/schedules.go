package ui

import (
	"fmt"
	"time"

	"weave/config"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	"weave/internal/view"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// schedulesScreen is the schedules screen, holding its own cursor behind the panel's one door.
type schedulesScreen struct {
	rowCursor
	rows []scheduleItem
}

type scheduleItem struct {
	schedule panel.Schedule
	nextRun  time.Time
}

func (i scheduleItem) key() string {
	return i.schedule.ID
}

// enter lands the cursor once the root has put the screen up.
func (w *schedulesScreen) enter(h host, s panelShared) {
	w.cursor = clampCursor(w.cursor, len(w.listRows(s)))
	w.place(h, s)
}

func (w *schedulesScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	return w.actions().dispatch(h, s, key.String())
}

func (w *schedulesScreen) list() screenList[scheduleItem] {
	return screenList[scheduleItem]{
		cursor: func() *rowCursor { return &w.rowCursor },
		rows:   w.listRows,
		name:   func(item scheduleItem) string { return item.schedule.Name },
		placed: w.place,
		quit:   func(k config.Keybindings) []string { return k.Schedules.Quit },
		back:   func(k config.Keybindings) []string { return k.Schedules.Back },
	}
}

// listRows is the list as drawn, or as it would be drawn when nothing has cached it.
func (w *schedulesScreen) listRows(s panelShared) []scheduleItem {
	if w.rows != nil {
		return w.rows
	}

	return w.buildRows(s)
}

func (w *schedulesScreen) buildRows(s panelShared) []scheduleItem {
	return narrowed(s.chrome.filter, narrowedParams[scheduleItem]{
		rows:  s.listing.schedules,
		depth: func(scheduleItem) int { return 0 },
		name:  func(item scheduleItem) string { return item.schedule.Name },
	})
}

func (w *schedulesScreen) selected(s panelShared) (scheduleItem, bool) {
	return rowAt(w.listRows(s), w.cursor)
}

// place names the row the cursor is on to the column.
func (w *schedulesScreen) place(h host, s panelShared) {
	h.placeColumn(screenSchedules, w.list().hovered(s))
}

// apply takes a fresh listing, keeping the cursor on the schedule it was on.
func (w *schedulesScreen) apply(h host, s panelShared, hovered string) {
	w.list().place(h, s, hovered)
}

func (w *schedulesScreen) details(s panelShared) details {
	item, ok := w.selected(s)
	if !ok {
		return details{}
	}

	return details{
		id:     item.schedule.ID,
		fields: scheduleFields(item.schedule, workflowByID(s.listing.workflows, item.schedule.WorkflowID)),
	}
}

// doc is the markdown drawn beside the list: the notice for an empty list, or the schedule the
// cursor is on.
func (w *schedulesScreen) doc(s panelShared) string {
	item, ok := w.selected(s)
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

func (w *schedulesScreen) toggle(h host, s panelShared) tea.Cmd {
	item, ok := w.selected(s)
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

func (w *schedulesScreen) newForm(h host, s panelShared) tea.Cmd {
	if len(s.listing.workflows) == 0 {
		h.report("Could not create a schedule", errNoWorkflow)

		return nil
	}

	values := panel.ScheduleConfig{
		Enabled:    true,
		WorkflowID: s.listing.workflows[0].ID,
	}

	return w.openForm(h, &values, nil)
}

// openForm puts up the schedule form, creating a schedule when target is nil and saving over the
// one it names otherwise.
func (w *schedulesScreen) openForm(h host, values *panel.ScheduleConfig, target *panel.Schedule) tea.Cmd {
	return h.openForm(redrawnForm(redrawnFormParams{
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

func (w *schedulesScreen) editForm(h host, s panelShared) tea.Cmd {
	item, ok := w.selected(s)
	if !ok {
		return nil
	}

	target := item.schedule
	if fresh, err := h.client().getSchedule(target.ID); err == nil && fresh != nil {
		target = *fresh
	}

	values := schedulePrefill(target)

	return w.openForm(h, &values, &target)
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

func (w *schedulesScreen) archiveConfirm(h host, s panelShared) tea.Cmd {
	item, ok := w.selected(s)
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

// applySchedules hands the screen a fresh listing.
func (m *model) applySchedules(fresh []scheduleItem) {
	hovered := m.schedules.list().hovered(m.shared())
	m.listing.schedules = fresh
	m.schedules.apply(m, m.shared(), hovered)
}

func (w *schedulesScreen) view(s panelShared) string {
	w.rows = w.buildRows(s)
	defer func() { w.rows = nil }()

	return s.draw.columnListView(w.listParams(s))
}

func (w *schedulesScreen) listParams(s panelShared) screenListParams {
	return screenListParams{
		title:  screenTitle(view.GlyphSchedule, "Schedules"),
		help:   w.help,
		column: "the record",
		rows:   len(w.listRows(s)),
		lines:  func(pane listPane) ([]string, int) { return w.lines(s, pane), w.cursor },
	}
}

func (w *schedulesScreen) help(s panelShared) keyMap {
	return w.actions().help(s)
}

// lines is the list as drawn: each row's cursor, icon, name and cron, or one line over no rows.
func (w *schedulesScreen) lines(s panelShared, pane listPane) []string {
	all := w.listRows(s)
	if len(all) == 0 {
		return []string{dimStyle.Render("  " + emptyLineOf(s.chrome.filter, "no schedules yet"))}
	}

	rows := make([]string, 0, len(all))
	for i, item := range all {
		selected := i == w.cursor
		prefix := "  "
		if selected {
			prefix = caretGlyph(pane.caret)
		}

		name := s.chrome.filter.highlight(paint.Panel, clipName(item.schedule.Name), nameStyle(selected && pane.caret != view.CaretAbsent, !item.schedule.Enabled))
		icon := paint.Panel.Style(scheduleTone(item.schedule.Enabled)).Render(scheduleIcon(item.schedule.Enabled))
		rows = append(rows, prefix+"  "+icon+" "+name+"  "+dimStyle.Render(item.schedule.Cron))
	}

	return rows
}

// schedulesAdapter is the schedules screen behind the panel's one door.
type schedulesAdapter struct{}

var (
	_ detailed = schedulesAdapter{}
	_ filtered = schedulesAdapter{}
	_ columned = schedulesAdapter{}
)

func (schedulesAdapter) update(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.schedules.update(&m, m.shared(), msg)

	return m, cmd
}

func (schedulesAdapter) view(m model) string {
	return m.schedules.view(m.shared())
}

func (schedulesAdapter) needs() screenNeeds {
	return screenNeeds{fromListing: true}
}

func (schedulesAdapter) open(m model, _ screenTarget) (tea.Model, tea.Cmd) {
	m.enterScreen(screenSchedules)
	m.schedules.enter(&m, m.shared())

	return m, nil
}

func (schedulesAdapter) takesEveryKey(model) bool {
	return false
}

func (schedulesAdapter) details(m model) details {
	return m.schedules.details(m.shared())
}

func (schedulesAdapter) detailsQuit(keys config.Keybindings) []string {
	return keys.Schedules.Quit
}

func (schedulesAdapter) detailsFit(m model) bool {
	s := m.shared()

	return s.draw.listFitsDetails(m.schedules.listParams(s))
}

func (schedulesAdapter) detailsRoom(m model) int {
	s := m.shared()

	return s.draw.listRecordRoom(m.schedules.listParams(s))
}

func (schedulesAdapter) hoveredKey(m model) string {
	return m.schedules.list().hovered(m.shared())
}

func (schedulesAdapter) placeAt(m *model, key string) {
	m.schedules.list().place(m, m.shared(), key)
}

func (schedulesAdapter) filterRows(m model) []filterRow {
	return m.schedules.list().filterRows(m.shared())
}

// column is the hovered schedule's own record drawn beside the list.
func (schedulesAdapter) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   func(m model) string { return m.schedules.doc(m.shared()) },
	}
}
