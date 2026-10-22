package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/column"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
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
	built screen.Derived[screen.ListingKey, []scheduleItem]
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

// Enter lands the cursor once the root has put the screen up.
func (w *schedulesScreen) Enter(in screen.Inputs, _ screen.Target) screen.Request {
	w.Row = cursor.Clamp(w.Row, len(w.listRows(in)))

	return screen.Settle{}
}

func (w *schedulesScreen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	if _, ok := msg.(tea.PasteMsg); ok && w.IsRenaming() {
		return w.updateRename(w.deps.register, w.deps.clients, msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	w.reconcileRename(in)
	if w.IsRenaming() {
		return w.updateRename(w.deps.register, w.deps.clients, msg)
	}

	return w.actions(in).DispatchTop(w.deps.top, w, w.deps.keys, key.String())
}

func (w *schedulesScreen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case api.RenameSettled:
		w.settleRename(in, msg.Row, msg.Err)

		return nil, true
	case screen.Relisted:
		return w.apply(in, w.list().Hovered(msg.Before(in))), true
	}

	return nil, false
}

func (w *schedulesScreen) list() keys.ScreenList[scheduleItem] {
	return keys.ScreenList[scheduleItem]{
		Keys:   w.deps.keys,
		Cursor: func() *list.Cursor { return &w.Cursor },
		Rows:   w.listRows,
		Name:   scheduleItemName,
		Fits:   scheduleItemFits,
		Moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (w *schedulesScreen) listRows(in screen.Inputs) []scheduleItem {
	return w.built.Read(in.ListingKey(), func() []scheduleItem {
		return list.Narrowed(in.Room.Pattern, list.NarrowedParams[scheduleItem]{
			Rows:  xslice.Map(in.Listing.Schedules, scheduleItemOf),
			Depth: func(scheduleItem) int { return 0 },
			Name:  scheduleItemName,
			Fits:  scheduleItemFits(in),
		})
	})
}

func (w *schedulesScreen) selected(in screen.Inputs) (scheduleItem, bool) {
	return list.At(w.listRows(in), w.Row)
}

// apply takes a fresh listing, keeping the cursor on the schedule it was on.
func (w *schedulesScreen) apply(in screen.Inputs, hovered string) screen.Request {
	placed := w.list().Place(in, hovered)
	w.reconcileRename(in)

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
func (w *schedulesScreen) newScheduleForm(in screen.Inputs) screen.Request {
	if len(in.Listing.Workflows) == 0 {
		return screen.Report{Summary: "Could not create a schedule", Err: panel.ErrNoWorkflow}
	}

	values := panel.ScheduleConfig{
		Kind:       schedulev1.ScheduleKind_SCHEDULE_KIND_TASK,
		Enabled:    true,
		WorkflowID: in.Listing.Workflows[0].ID,
	}
	if orchestrators := in.Listing.LiveOrchestrators(); len(orchestrators) > 0 {
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

func (w *schedulesScreen) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Help:     w.help(in),
		Empty:    "no schedules yet",
		Rows:     len(w.listRows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return w.snapshot(in, pane) },
	}
}

func (w *schedulesScreen) help(in screen.Inputs) legend.Help {
	return w.actions(in).Help(w, w.deps.keys)
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

func (w *schedulesScreen) DoesRenderFromListing() bool {
	return true
}

func (w *schedulesScreen) TakesEveryKey() bool {
	return w.IsRenaming()
}

func (w *schedulesScreen) ActiveList() keys.KeyedList {
	return w.list()
}

func (w *schedulesScreen) HoveredName(in screen.Inputs) string {
	return w.list().HoveredName(in)
}

// Column is the hovered schedule's own record rendered beside the list.
func (w *schedulesScreen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return w.docRows(in, width)
		},
	}
}

func (w *schedulesScreen) openRename(in screen.Inputs) screen.Request {
	if item, ok := w.selected(in); ok {
		w.renaming = renameScheduleRow(item)
	}

	return nil
}

func (w *schedulesScreen) reconcileRename(in screen.Inputs) {
	reconcileRename(&w.renameSlot, w.listRows(in), w.list().Hovered(in), scheduleItemName)
}

func (w *schedulesScreen) settleRename(in screen.Inputs, row string, err error) {
	if w.settleField(row, err) {
		w.reconcileRename(in)
	}
}

func (w *schedulesScreen) Dispatches() []keys.Table {
	return []keys.Table{w.actions(screen.Inputs{}), w.archived.actions(screen.Inputs{})}
}
