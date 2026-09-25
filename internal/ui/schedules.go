package ui

import (
	"fmt"
	"time"

	"komrad/config"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
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
func (w *schedulesScreen) enter(h host, s panelShared, _ screenTarget) tea.Cmd {
	w.cursor = clampCursor(w.cursor, len(w.listRows(s)))

	return h.waitToStage()
}

func (w *schedulesScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
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
		moved:  func(h host, _ panelShared) tea.Cmd { return h.waitToStage() },
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

// docRows is the document drawn beside the list at a width: the notice for an empty list, or
// the schedule the cursor is on with the firings River holds for it between its next run and
// its prompt.
func (w *schedulesScreen) docRows(s panelShared, width int) []string {
	item, ok := w.selected(s)
	if !ok {
		return paint.DocStyledRows(noSchedulesMarkdown, width, columnStyle())
	}

	return scheduleDocRows(item, width)
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

func (w *schedulesScreen) view(s panelShared) string {
	w.rows = w.buildRows(s)
	defer func() { w.rows = nil }()

	return s.draw.columnListView(w.listParams(s))
}

func (w *schedulesScreen) listParams(s panelShared) screenListParams {
	return screenListParams{
		title: screenTitle(view.GlyphSchedule, "Schedules"),
		help:  w.help,
		rows:  len(w.listRows(s)),
		lines: func(pane listPane) ([]string, int) { return w.lines(s, pane), w.cursor },
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

var (
	_ detailed = (*schedulesScreen)(nil)
	_ filtered = (*schedulesScreen)(nil)
	_ columned = (*schedulesScreen)(nil)
)

func (w *schedulesScreen) doesDrawFromListing() bool {
	return true
}

func (w *schedulesScreen) takesEveryKey() bool {
	return false
}

func (w *schedulesScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Schedules.Quit
}

func (w *schedulesScreen) detailsFit(s panelShared) bool {
	return s.draw.listFitsDetails(w.listParams(s))
}

func (w *schedulesScreen) detailsRoom(s panelShared) int {
	return s.draw.listRecordRoom(w.listParams(s))
}

func (w *schedulesScreen) hoveredKey(s panelShared) string {
	return w.list().hovered(s)
}

func (w *schedulesScreen) placeAt(h host, s panelShared, key string) {
	w.list().place(h, s, key)
}

func (w *schedulesScreen) filterRows(s panelShared) []filterRow {
	return w.list().filterRows(s)
}

// column is the hovered schedule's own record drawn beside the list.
func (w *schedulesScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		docRows: func(m model, width int) []string {
			return m.schedules.docRows(m.shared(), width)
		},
	}
}
