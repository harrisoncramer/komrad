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
func (w *schedulesScreen) enter(m *model, _ screenTarget) tea.Cmd {
	w.cursor = cursor.Clamp(w.cursor, len(w.listRows(m)))

	return m.waitToStage()
}

func (w *schedulesScreen) update(m *model, msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.PasteMsg); ok && w.isRenaming() {
		return w.updateRename(m, msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	w.reconcileRename(m)
	if w.isRenaming() {
		return w.updateRename(m, msg)
	}

	return w.actions().dispatch(m, key.String())
}

func (w *schedulesScreen) list() screenList[scheduleItem] {
	return screenList[scheduleItem]{
		cursor: func() *rowCursor { return &w.rowCursor },
		rows:   w.listRows,
		name:   scheduleItemName,
		moved:  func(m *model) tea.Cmd { return m.waitToStage() },
		quit:   func(k config.Keybindings) []string { return k.Schedules.Quit },
		back:   func(k config.Keybindings) []string { return k.Schedules.Back },
	}
}

// listRows is the list as drawn, or as it would be drawn when nothing has cached it.
func (w *schedulesScreen) listRows(m *model) []scheduleItem {
	if w.rows != nil {
		return w.rows
	}

	return w.buildRows(m)
}

func (w *schedulesScreen) buildRows(m *model) []scheduleItem {
	return narrowed(m.filter, narrowedParams[scheduleItem]{
		rows:  m.listing.schedules,
		depth: func(scheduleItem) int { return 0 },
		name:  scheduleItemName,
	})
}

func (w *schedulesScreen) selected(m *model) (scheduleItem, bool) {
	return rowAt(w.listRows(m), w.cursor)
}

// apply takes a fresh listing, keeping the cursor on the schedule it was on.
func (w *schedulesScreen) apply(m *model, hovered string) {
	w.list().place(m, hovered)
	w.reconcileRename(m)
}

func (w *schedulesScreen) details(m *model) details {
	item, ok := w.selected(m)
	if !ok {
		return details{}
	}

	return scheduleDetails(m, item)
}

func scheduleDetails(m *model, item scheduleItem) details {
	return details{
		id: item.schedule.ID,
		fields: scheduleFields(item.schedule, scheduleTarget{
			workflow:     workflowByID(m.listing.workflows, item.schedule.WorkflowID),
			orchestrator: m.listing.orchestratorLabel(item.schedule.OrchestratorID),
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
func (w *schedulesScreen) docRows(m *model, width int) []string {
	item, ok := w.selected(m)
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
func toggleSchedule(m *model, schedule panel.Schedule) tea.Cmd {
	doing, failure := "pausing "+schedule.Name, fmt.Sprintf("Could not pause schedule %q", schedule.Name)
	if !schedule.Enabled {
		doing, failure = "resuming "+schedule.Name, fmt.Sprintf("Could not resume schedule %q", schedule.Name)
	}

	return m.writeCmd(doing, failure, func(a api) error {
		return a.setScheduleEnabled(schedule.ID, !schedule.Enabled)
	})
}

// newScheduleForm puts up the form for a new schedule.
func newScheduleForm(m *model) tea.Cmd {
	if len(m.listing.workflows) == 0 {
		m.report("Could not create a schedule", errNoWorkflow)

		return nil
	}

	values := panel.ScheduleConfig{
		Kind:       schedulev1.ScheduleKind_SCHEDULE_KIND_IMPLEMENTER,
		Enabled:    true,
		WorkflowID: m.listing.workflows[0].ID,
	}
	if orchestrators := m.listing.liveOrchestrators(); len(orchestrators) > 0 {
		values.OrchestratorID = orchestrators[0].ID
	}

	return openScheduleForm(m, &values, nil)
}

// openScheduleForm puts up the schedule form, creating a schedule when target is nil and saving
// over the one it names otherwise.
func openScheduleForm(m *model, values *panel.ScheduleConfig, target *panel.Schedule) tea.Cmd {
	drawnKind := values.Kind

	return m.openForm(redrawnForm(redrawnFormParams{
		Kind: formSchedule,
		Build: func(m *model) (*huh.Form, []string) {
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
		Submit: holdSubmit(func(m *model) tea.Cmd {
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

			return cmd
		}),
	}))
}

// editScheduleForm puts up the form over a stored schedule.
func editScheduleForm(m *model, schedule panel.Schedule) tea.Cmd {
	values := schedulePrefill(schedule)

	return openScheduleForm(m, &values, &schedule)
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

func (m *model) archiveSchedule(schedule panel.Schedule) tea.Cmd {
	return m.writeCmd(
		"archiving "+schedule.Name,
		fmt.Sprintf("Could not archive schedule %q", schedule.Name),
		func(a api) error { return a.archiveSchedule(schedule.ID) },
	)
}

func (w *schedulesScreen) view(m *model) string {
	w.rows = w.buildRows(m)
	defer func() { w.rows = nil }()

	return m.columnListView(w.listParams(m))
}

func (w *schedulesScreen) listParams(m *model) screenListParams {
	return screenListParams{
		help:  w.help,
		rows:  len(w.listRows(m)),
		lines: func(pane listPane) ([]string, int) { return w.lines(m, pane), w.cursor },
	}
}

func (w *schedulesScreen) help(m *model) keyMap {
	return w.actions().help(m)
}

// lines is the list as drawn: each row's cursor, icon, name and cron, or one line over no rows.
func (w *schedulesScreen) lines(m *model, pane listPane) []string {
	return scheduleLines(m, pane, scheduleLinesParams{
		rows:     w.listRows(m),
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
func scheduleLines(m *model, pane listPane, params scheduleLinesParams) []string {
	if len(params.rows) == 0 {
		return []string{dimStyle().Render("  " + emptyLineOf(m.filter, params.idle))}
	}

	rows := make([]string, 0, len(params.rows))
	for i, item := range params.rows {
		selected := i == params.cursor
		prefix := "  "
		if selected {
			prefix = caretGlyph(pane.caret)
		}

		style := nameStyle(selected && pane.caret != view.CaretAbsent, !item.schedule.Enabled)
		name := m.filter.highlight(paint.Panel, clipName(item.schedule.Name), style)
		if e := params.renaming; e != nil && e.key == item.key() {
			name = drawnName(e, style)
		}
		icon := paint.Panel.Style(scheduleTone(item.schedule.Enabled)).Render(scheduleIcon(m.glyphs, item.schedule.Enabled))
		rows = append(rows, prefix+"  "+icon+" "+name+"  "+dimStyle().Render(item.schedule.Cron))
	}

	return rows
}

func (w *schedulesScreen) doesDrawFromListing() bool {
	return true
}

func (w *schedulesScreen) takesEveryKey() bool {
	return w.isRenaming()
}

func (w *schedulesScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Schedules.Quit
}

func (w *schedulesScreen) detailsFit(m *model) bool {
	return m.listFitsDetails(w.listParams(m))
}

func (w *schedulesScreen) detailsRoom(m *model) int {
	return m.listRecordRoom(w.listParams(m))
}

func (w *schedulesScreen) hoveredKey(m *model) string {
	return w.list().hovered(m)
}

func (w *schedulesScreen) hoveredName(m *model) string {
	return w.list().hoveredName(m)
}

func (w *schedulesScreen) placeAt(m *model, key string) {
	w.list().place(m, key)
}

func (w *schedulesScreen) filterRows(m *model) []filterRow {
	return w.list().filterRows(m)
}

// column is the hovered schedule's own record drawn beside the list.
func (w *schedulesScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		read:  isAlwaysRead,
		docRows: func(m *model, width int) []string {
			return m.schedules.docRows(m, width)
		},
	}
}

func (w *schedulesScreen) openRename(m *model) tea.Cmd {
	if item, ok := w.selected(m); ok {
		w.renaming = renameScheduleRow(item)
	}

	return nil
}

func (w *schedulesScreen) reconcileRename(m *model) {
	reconcileRename(&w.renameSlot, w.listRows(m), w.hoveredKey(m), scheduleItemName)
}

func (w *schedulesScreen) settleRename(m *model, row string, err error) {
	if w.settleField(row, err) {
		w.reconcileRename(m)
	}
}

func (w *schedulesScreen) spec() screenSpec {
	return screenSpec{
		screenModel:   w,
		column:        w.column,
		detailed:      w,
		filtered:      w,
		hoveredName:   w.hoveredName,
		screenMenu:    w.screenMenu,
		itemMenu:      w.itemMenu,
		typedLine:     w.typedLine,
		isRenaming:    w.isRenaming,
		stageResting:  w.stageResting,
		dispatches:    func(*model) []dispatching { return []dispatching{w.actions()} },
		opensTerminal: true,
	}
}
