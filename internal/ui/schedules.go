package ui

import (
	"fmt"
	"time"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
	schedulev1 "komrad/proto/gen/schedule/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// schedulesScreen is the schedules screen, holding its own cursor behind the panel's one door.
type schedulesScreen struct {
	rowCursor
	rows []scheduleItem
	renameSlot
}

type scheduleItem struct {
	schedule panel.Schedule
	nextRun  time.Time
}

func (i scheduleItem) key() string {
	return i.schedule.ID
}

func scheduleItemName(item scheduleItem) string {
	return item.schedule.Name
}

// enter lands the cursor once the root has put the screen up.
func (w *schedulesScreen) enter(h host, s panelShared, _ screenTarget) tea.Cmd {
	w.cursor = cursor.Clamp(w.cursor, len(w.listRows(s)))

	return h.waitToStage()
}

func (w *schedulesScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.PasteMsg); ok && w.isRenaming() {
		return w.updateRename(h, msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	w.reconcileRename(s)
	if w.isRenaming() {
		return w.updateRename(h, msg)
	}

	return w.actions().dispatch(h, s, key.String())
}

func (w *schedulesScreen) list() screenList[scheduleItem] {
	return screenList[scheduleItem]{
		cursor: func() *rowCursor { return &w.rowCursor },
		rows:   w.listRows,
		name:   scheduleItemName,
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
		name:  scheduleItemName,
	})
}

func (w *schedulesScreen) selected(s panelShared) (scheduleItem, bool) {
	return rowAt(w.listRows(s), w.cursor)
}

// apply takes a fresh listing, keeping the cursor on the schedule it was on.
func (w *schedulesScreen) apply(h host, s panelShared, hovered string) {
	w.list().place(h, s, hovered)
	w.reconcileRename(s)
}

func (w *schedulesScreen) details(s panelShared) details {
	item, ok := w.selected(s)
	if !ok {
		return details{}
	}

	return scheduleDetails(s, item)
}

func scheduleDetails(s panelShared, item scheduleItem) details {
	return details{
		id: item.schedule.ID,
		fields: scheduleFields(item.schedule, scheduleTarget{
			workflow:     workflowByID(s.listing.workflows, item.schedule.WorkflowID),
			orchestrator: s.listing.orchestratorLabel(item.schedule.OrchestratorID),
		}),
	}
}

func (l listing) orchestratorLabel(id string) string {
	for _, row := range l.agents {
		if row.Agent.ID == id {
			return row.Agent.Name
		}
	}
	for _, row := range l.archivedAgents {
		if row.Agent.ID == id {
			return row.Agent.Name + " (archived)"
		}
	}

	return "— (gone)"
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

// toggleSchedule pauses the schedule, or resumes it when it is paused.
func toggleSchedule(h host, schedule panel.Schedule) tea.Cmd {
	doing, failure := "pausing "+schedule.Name, fmt.Sprintf("Could not pause schedule %q", schedule.Name)
	if !schedule.Enabled {
		doing, failure = "resuming "+schedule.Name, fmt.Sprintf("Could not resume schedule %q", schedule.Name)
	}

	return h.writeCmd(doing, failure, func(a api) error {
		return a.setScheduleEnabled(schedule.ID, !schedule.Enabled)
	})
}

func (w *schedulesScreen) newForm(h host, s panelShared) tea.Cmd {
	if len(s.listing.workflows) == 0 {
		h.report("Could not create a schedule", errNoWorkflow)

		return nil
	}

	values := panel.ScheduleConfig{
		Kind:       schedulev1.ScheduleKind_SCHEDULE_KIND_IMPLEMENTER,
		Enabled:    true,
		WorkflowID: s.listing.workflows[0].ID,
	}
	if orchestrators := s.listing.liveOrchestrators(); len(orchestrators) > 0 {
		values.OrchestratorID = orchestrators[0].ID
	}

	return w.openForm(h, &values, nil)
}

// openForm puts up the schedule form, creating a schedule when target is nil and saving over the
// one it names otherwise.
func (w *schedulesScreen) openForm(h host, values *panel.ScheduleConfig, target *panel.Schedule) tea.Cmd {
	drawnKind := values.Kind

	return h.openForm(redrawnForm(redrawnFormParams{
		Kind: formSchedule,
		Build: func(m model) (*huh.Form, []string) {
			drawnKind = values.Kind

			return scheduleForm(scheduleFormParams{
				values:        values,
				workflows:     m.listing.workflows,
				orchestrators: m.listing.liveOrchestrators(),
				keys:          formKeys(),
			})
		},
		Reshaped: func() bool { return values.Kind != drawnKind },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit: holdSubmit(func(m model) (model, tea.Cmd) {
			var cmd tea.Cmd
			if target == nil {
				cmd = m.writeCmd("creating schedule "+values.Name, "Could not create the schedule", func(a api) error { return a.createSchedule(*values) })
			} else {
				cmd = m.writeCmd(
					"saving "+target.Name,
					fmt.Sprintf("Could not save schedule %q", target.Name),
					func(a api) error { return a.updateSchedule(target.ID, *values) },
				)
			}

			return m, cmd
		}),
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
		Name:           schedule.Name,
		Cron:           schedule.Cron,
		Timezone:       schedule.Timezone,
		Enabled:        schedule.Enabled,
		HideAgents:     schedule.HideAgents,
		Prompt:         schedule.Prompt,
		WorkflowID:     schedule.WorkflowID,
		Kind:           schedule.Kind,
		OrchestratorID: schedule.OrchestratorID,
	}
}

// liveOrchestrators is the orchestrators a schedule may mail.
func (l listing) liveOrchestrators() []panel.Agent {
	var orchestrators []panel.Agent
	for _, row := range l.agents {
		if row.Agent.Role == panel.RoleOrchestrator {
			orchestrators = append(orchestrators, row.Agent)
		}
	}

	return orchestrators
}

func (m model) archiveSchedule(schedule panel.Schedule) (model, tea.Cmd) {
	return m, m.writeCmd(
		"archiving "+schedule.Name,
		fmt.Sprintf("Could not archive schedule %q", schedule.Name),
		func(a api) error { return a.archiveSchedule(schedule.ID) },
	)
}

func (w *schedulesScreen) view(s panelShared) string {
	w.rows = w.buildRows(s)
	defer func() { w.rows = nil }()

	return s.draw.columnListView(w.listParams(s))
}

func (w *schedulesScreen) listParams(s panelShared) screenListParams {
	return screenListParams{
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
	return scheduleLines(s, pane, scheduleLinesParams{
		rows:     w.listRows(s),
		cursor:   w.cursor,
		idle:     "no schedules yet",
		renaming: w.renamingField(),
	})
}

type scheduleLinesParams struct {
	rows     []scheduleItem
	cursor   int
	idle     string
	renaming *nameEdit
}

// scheduleLines draws schedule rows with the cursor on one, or the idle line over none.
func scheduleLines(s panelShared, pane listPane, params scheduleLinesParams) []string {
	if len(params.rows) == 0 {
		return []string{dimStyle().Render("  " + emptyLineOf(s.chrome.filter, params.idle))}
	}

	rows := make([]string, 0, len(params.rows))
	for i, item := range params.rows {
		selected := i == params.cursor
		prefix := "  "
		if selected {
			prefix = caretGlyph(pane.caret)
		}

		style := nameStyle(selected && pane.caret != view.CaretAbsent, !item.schedule.Enabled)
		name := s.chrome.filter.highlight(paint.Panel, clipName(item.schedule.Name), style)
		if e := params.renaming; e != nil && e.key == item.key() {
			name = drawnName(e, style)
		}
		icon := paint.Panel.Style(scheduleTone(item.schedule.Enabled)).Render(scheduleIcon(s.glyphs, item.schedule.Enabled))
		rows = append(rows, prefix+"  "+icon+" "+name+"  "+dimStyle().Render(item.schedule.Cron))
	}

	return rows
}

var (
	_ detailed = (*schedulesScreen)(nil)
	_ filtered = (*schedulesScreen)(nil)
	_ columned = (*schedulesScreen)(nil)
	_ renamer  = (*schedulesScreen)(nil)
)

func (w *schedulesScreen) doesDrawFromListing() bool {
	return true
}

func (w *schedulesScreen) takesEveryKey() bool {
	return w.isRenaming()
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

func (w *schedulesScreen) hoveredName(s panelShared) string {
	return w.list().hoveredName(s)
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
		read:  isAlwaysRead,
		docRows: func(m model, width int) []string {
			return m.schedules.docRows(m.shared(), width)
		},
	}
}

func (w *schedulesScreen) openRename(s panelShared) tea.Cmd {
	if item, ok := w.selected(s); ok {
		w.renaming = renameScheduleRow(item)
	}

	return nil
}

func (w *schedulesScreen) reconcileRename(s panelShared) {
	reconcileRename(&w.renameSlot, w.listRows(s), w.hoveredKey(s), scheduleItemName)
}

func (w *schedulesScreen) settleRename(s panelShared, row string, err error) {
	if w.settleField(row, err) {
		w.reconcileRename(s)
	}
}
