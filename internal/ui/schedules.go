package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/column"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	"komrad/internal/vim"
	"komrad/lib/xslice"
	schedulev1 "komrad/proto/gen/schedule/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// schedulesDeps is what the schedules screens are given at launch.
type schedulesDeps struct {
	keys     config.Keybindings
	register *vim.Register
	clients  api.Clients
	top      *keys.Top
}

// schedulesScreen is the schedules screen, holding its own cursor behind the panel's one door.
type schedulesScreen struct {
	deps schedulesDeps
	list.Cursor
	built derived[listingKey, []scheduleItem]
	renameSlot
	kind      schedulesKind
	kindFocus bool
	archived  archivedSchedules
}

type scheduleItem struct {
	schedule panel.Schedule
}

func (i scheduleItem) Key() string {
	return i.schedule.ID
}

func scheduleItemOf(schedule panel.Schedule) scheduleItem {
	return scheduleItem{schedule: schedule}
}

func scheduleItemName(item scheduleItem) string {
	return item.schedule.Name
}

// enter lands the cursor once the root has put the screen up.
func (w *schedulesScreen) enter(m *model, in screen.Inputs, _ screen.Target) screen.Request {
	w.Row = cursor.Clamp(w.Row, len(w.listRows(m, in)))

	return screen.Settle{}
}

func (w *schedulesScreen) update(m *model, in screen.Inputs, msg tea.Msg) screen.Request {
	if _, ok := msg.(tea.PasteMsg); ok && w.isRenaming() {
		return w.updateRename(w.deps.register, w.deps.clients, msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	w.reconcileRename(m, in)
	if w.isRenaming() {
		return w.updateRename(w.deps.register, w.deps.clients, msg)
	}

	return w.actions().DispatchTop(w.deps.top, m, w.deps.keys, key.String())
}

func (w *schedulesScreen) receive(m *model, in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case api.RenameSettled:
		w.settleRename(m, in, msg.Row, msg.Err)

		return nil, true
	case screen.Relisted:
		return w.apply(m, in, w.list().hovered(m, msg.Before(in))), true
	}

	return nil, false
}

func (w *schedulesScreen) list() screenList[scheduleItem] {
	return screenList[scheduleItem]{
		keys:   w.deps.keys,
		top:    w.deps.top,
		cursor: func() *list.Cursor { return &w.Cursor },
		rows:   w.listRows,
		name:   scheduleItemName,
		fits:   (*model).scheduleItemFits,
		moved:  func(*model, screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (w *schedulesScreen) listRows(m *model, in screen.Inputs) []scheduleItem {
	return w.built.read(m.listingKey(in.Listing), func() []scheduleItem {
		return narrowed(m.filter, narrowedParams[scheduleItem]{
			rows:  xslice.Map(in.Listing.Schedules, scheduleItemOf),
			depth: func(scheduleItem) int { return 0 },
			name:  scheduleItemName,
			fits:  m.scheduleItemFits(in.Listing),
		})
	})
}

func (w *schedulesScreen) selected(m *model, in screen.Inputs) (scheduleItem, bool) {
	return list.At(w.listRows(m, in), w.Row)
}

// apply takes a fresh listing, keeping the cursor on the schedule it was on.
func (w *schedulesScreen) apply(m *model, in screen.Inputs, hovered string) screen.Request {
	placed := w.list().place(m, in, hovered)
	w.reconcileRename(m, in)

	return placed
}

func orchestratorLabel(l panel.Listing, id string) string {
	for _, row := range l.Agents {
		if row.Agent.ID == id {
			return row.Agent.Name
		}
	}
	for _, row := range l.ArchivedAgents {
		if row.Agent.ID == id {
			return row.Agent.Name + " (archived)"
		}
	}

	return "— (gone)"
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
func (w *schedulesScreen) toggleSchedule(schedule panel.Schedule) screen.Request {
	doing, failure := "pausing "+schedule.Name, fmt.Sprintf("Could not pause schedule %q", schedule.Name)
	if !schedule.Enabled {
		doing, failure = "resuming "+schedule.Name, fmt.Sprintf("Could not resume schedule %q", schedule.Name)
	}

	return w.deps.clients.Write(doing, failure, func(a api.Clients) error {
		return a.SetScheduleEnabled(schedule.ID, !schedule.Enabled)
	})
}

// newScheduleForm puts up the form for a new schedule.
func (w *schedulesScreen) newScheduleForm(m *model) screen.Request {
	if len(m.listing.Workflows) == 0 {
		return screen.Report{Summary: "Could not create a schedule", Err: errNoWorkflow}
	}

	values := panel.ScheduleConfig{
		Kind:       schedulev1.ScheduleKind_SCHEDULE_KIND_TASK,
		Enabled:    true,
		WorkflowID: m.listing.Workflows[0].ID,
	}
	if orchestrators := m.listing.LiveOrchestrators(); len(orchestrators) > 0 {
		values.OrchestratorID = orchestrators[0].ID
	}

	return w.openScheduleForm(&values, nil)
}

// openScheduleForm puts up the schedule form, creating a schedule when target is nil and saving
// over the one it names otherwise.
func (w *schedulesScreen) openScheduleForm(values *panel.ScheduleConfig, target *panel.Schedule) screen.Request {
	renderedKind := values.Kind

	return form.Open{Form: form.Form{
		Build: func(in form.Inputs) (*huh.Form, []string) {
			renderedKind = values.Kind

			return scheduleForm(scheduleFormParams{
				values:        values,
				workflows:     in.Listing.Workflows,
				orchestrators: in.Listing.LiveOrchestrators(),
				keys:          fixedKeys(),
			})
		},
		Reshaped: func() bool { return values.Kind != renderedKind },
		Editable: []form.Editable{{Key: "prompt", Value: &values.Prompt}},
		Submit: func(form.Inputs) screen.Request {
			if target == nil {
				return w.deps.clients.Write("creating schedule "+values.Name, "Could not create the schedule", func(a api.Clients) error { return a.CreateSchedule(*values) })
			}

			return w.deps.clients.Write(
				"saving "+target.Name,
				fmt.Sprintf("Could not save schedule %q", target.Name),
				func(a api.Clients) error { return a.UpdateSchedule(target.ID, *values) },
			)
		},
	}}
}

// editScheduleForm puts up the form over a stored schedule.
func (w *schedulesScreen) editScheduleForm(schedule panel.Schedule) screen.Request {
	if !panel.IsKnownScheduleKind(schedule.Kind) {
		return screen.Report{Summary: fmt.Sprintf("Could not edit schedule %q", schedule.Name), Err: panel.ErrUnknownScheduleKind}
	}

	values := schedulePrefill(schedule)

	return w.openScheduleForm(&values, &schedule)
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

func (w *schedulesScreen) archiveSchedule(schedule panel.Schedule) screen.Request {
	return w.deps.clients.AwaitingWrite(api.AwaitingWriteParams{
		Doing:   "archiving " + schedule.Name,
		Summary: fmt.Sprintf("Could not archive schedule %q", schedule.Name),
		Awaits:  screen.Awaits{Gone: screen.RowsIn(screen.ListSchedules, schedule.ID)},
		Write:   func(a api.Clients) error { return a.ArchiveSchedule(schedule.ID) },
	})
}

func (w *schedulesScreen) listParams(m *model, in screen.Inputs) screenListParams {
	return screenListParams{
		help:     w.help,
		empty:    "no schedules yet",
		rows:     len(w.listRows(m, in)),
		snapshot: func(pane list.Pane) view.Snapshot { return w.snapshot(m, in, pane) },
	}
}

func (w *schedulesScreen) help(m *model) keyMap {
	return m.helpOf(w.actions())
}

var (
	_ recordRenderer    = (*schedulesScreen)(nil)
	_ activeLister      = (*schedulesScreen)(nil)
	_ hoveredNamer      = (*schedulesScreen)(nil)
	_ screenMenuBuilder = (*schedulesScreen)(nil)
	_ itemMenuBuilder   = (*schedulesScreen)(nil)
	_ lineTyper         = (*schedulesScreen)(nil)
	_ renamer           = (*schedulesScreen)(nil)
	_ kindSwitcher      = (*schedulesScreen)(nil)
)

func (w *schedulesScreen) doesRenderFromListing() bool {
	return true
}

func (w *schedulesScreen) takesEveryKey() bool {
	return w.isRenaming()
}

func (w *schedulesScreen) activeList() keyedList {
	return w.list()
}

func (w *schedulesScreen) hoveredName(m *model, in screen.Inputs) string {
	return w.list().hoveredName(m, in)
}

// column is the hovered schedule's own record rendered beside the list.
func (w *schedulesScreen) column(m *model, in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: columnFloor,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return m.schedules.docRows(m, in, width)
		},
	}
}

func (w *schedulesScreen) openRename(m *model, in screen.Inputs) screen.Request {
	if item, ok := w.selected(m, in); ok {
		w.renaming = renameScheduleRow(item)
	}

	return nil
}

func (w *schedulesScreen) reconcileRename(m *model, in screen.Inputs) {
	reconcileRename(&w.renameSlot, w.listRows(m, in), w.list().hovered(m, in), scheduleItemName)
}

func (w *schedulesScreen) settleRename(m *model, in screen.Inputs, row string, err error) {
	if w.settleField(row, err) {
		w.reconcileRename(m, in)
	}
}

func (w *schedulesScreen) dispatches(*model) []keys.Table {
	return []keys.Table{w.actions(), w.archived.actions()}
}
