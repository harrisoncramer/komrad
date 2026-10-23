package schedules

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

// Deps is what the schedules screens are given at launch.
type Deps struct {
	Keys     config.Keybindings
	Register *vim.Register
	Clients  api.Clients
	Top      *keys.Top
}

// Screen is the schedules screen, holding its own cursor behind the panel's one door.
type Screen struct {
	deps Deps
	list.Cursor
	built     screen.Derived[screen.ListingKey, []row]
	renaming  *rename
	kind      kind
	kindFocus bool
	archive   archive
}

// New is the schedules screen built from what it is given at launch.
func New(deps Deps) Screen {
	return Screen{deps: deps, archive: archive{deps: deps}}
}

type row struct {
	schedule panel.Schedule
}

func (r row) Key() string {
	return r.schedule.ID
}

func rowOf(schedule panel.Schedule) row {
	return row{schedule: schedule}
}

func rowName(item row) string {
	return item.schedule.Name
}

// Enter lands the cursor once the root has put the screen up.
func (w *Screen) Enter(in screen.Inputs, _ screen.Target) screen.Request {
	w.Row = cursor.Clamp(w.Row, len(w.listRows(in)))

	return screen.Settle{}
}

// Update sends a key to the rename being typed, or to the screen's table.
func (w *Screen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	if _, ok := msg.(tea.PasteMsg); ok && w.IsRenaming() {
		return w.updateRename(msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	w.reconcileRename(in)
	if w.IsRenaming() {
		return w.updateRename(msg)
	}

	return w.actions(in).DispatchTop(w.deps.Top, w, w.deps.Keys, key.String())
}

// Receive takes a rename landing and the listing moving.
func (w *Screen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case api.RenameSettled:
		w.settleRename(in, msg.Row, msg.Err)

		return nil, true
	case screen.Relisted:
		return w.apply(in, w.list().Hovered(msg.Before(in))), true
	}

	return nil, false
}

func (w *Screen) list() keys.ScreenList[row] {
	return keys.ScreenList[row]{
		Keys:   w.deps.Keys,
		Cursor: func() *list.Cursor { return &w.Cursor },
		Rows:   w.listRows,
		Name:   rowName,
		Fits:   rowFits,
		Moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

func (w *Screen) listRows(in screen.Inputs) []row {
	return w.built.Read(in.ListingKey(), func() []row {
		return list.Narrowed(in.Room.Pattern, list.NarrowedParams[row]{
			Rows:  xslice.Map(in.Listing.Schedules, rowOf),
			Depth: func(row) int { return 0 },
			Name:  rowName,
			Fits:  rowFits(in),
		})
	})
}

func (w *Screen) selected(in screen.Inputs) (row, bool) {
	return list.At(w.listRows(in), w.Row)
}

// apply takes a fresh listing, keeping the cursor on the schedule it was on.
func (w *Screen) apply(in screen.Inputs, hovered string) screen.Request {
	placed := w.list().Place(in, hovered)
	w.reconcileRename(in)

	return placed
}

func orchestratorLabel(l panel.Listing, id string) string {
	for _, listed := range l.Agents {
		if listed.Agent.ID == id {
			return listed.Agent.Name
		}
	}
	for _, listed := range l.ArchivedAgents {
		if listed.Agent.ID == id {
			return listed.Agent.Name + " (archived)"
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
func (w *Screen) toggleSchedule(schedule panel.Schedule) screen.Request {
	doing, failure := "pausing "+schedule.Name, fmt.Sprintf("Could not pause schedule %q", schedule.Name)
	if !schedule.Enabled {
		doing, failure = "resuming "+schedule.Name, fmt.Sprintf("Could not resume schedule %q", schedule.Name)
	}

	return w.deps.Clients.Write(doing, failure, func(a api.Clients) error {
		return a.SetScheduleEnabled(schedule.ID, !schedule.Enabled)
	})
}

// newScheduleForm puts up the form for a new schedule.
func (w *Screen) newScheduleForm(in screen.Inputs) screen.Request {
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

// openScheduleForm puts up the schedule form, creating a schedule when stored is nil and saving
// over the one it names otherwise.
func (w *Screen) openScheduleForm(values *panel.ScheduleConfig, stored *panel.Schedule) screen.Request {
	renderedKind := values.Kind

	return form.Open{Form: form.Form{
		Build: func(in form.Inputs) (*huh.Form, []string) {
			renderedKind = values.Kind

			return buildForm(buildFormParams{
				values:        values,
				workflows:     in.Listing.Workflows,
				orchestrators: in.Listing.LiveOrchestrators(),
				keys:          config.Fixed(),
			})
		},
		Reshaped: func() bool { return values.Kind != renderedKind },
		Editable: []form.Editable{{Key: "prompt", Value: &values.Prompt}},
		Submit: func(form.Inputs) screen.Request {
			if stored == nil {
				return w.deps.Clients.Write("creating schedule "+values.Name, "Could not create the schedule", func(a api.Clients) error { return a.CreateSchedule(*values) })
			}

			return w.deps.Clients.Write(
				"saving "+stored.Name,
				fmt.Sprintf("Could not save schedule %q", stored.Name),
				func(a api.Clients) error { return a.UpdateSchedule(stored.ID, *values) },
			)
		},
	}}
}

// editScheduleForm puts up the form over a stored schedule.
func (w *Screen) editScheduleForm(schedule panel.Schedule) screen.Request {
	if !panel.IsKnownScheduleKind(schedule.Kind) {
		return screen.Report{Summary: fmt.Sprintf("Could not edit schedule %q", schedule.Name), Err: panel.ErrUnknownScheduleKind}
	}

	values := prefill(schedule)

	return w.openScheduleForm(&values, &schedule)
}

// prefill is the schedule form as the stored schedule leaves it.
func prefill(schedule panel.Schedule) panel.ScheduleConfig {
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

func (w *Screen) archiveSchedule(schedule panel.Schedule) screen.Request {
	return w.deps.Clients.AwaitingWrite(api.AwaitingWriteParams{
		Doing:   "archiving " + schedule.Name,
		Summary: fmt.Sprintf("Could not archive schedule %q", schedule.Name),
		Awaits:  screen.Awaits{Gone: screen.RowsIn(screen.ListSchedules, schedule.ID)},
		Write:   func(a api.Clients) error { return a.ArchiveSchedule(schedule.ID) },
	})
}

// ListParams is the list of schedules as the root renders it.
func (w *Screen) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Help:     w.help(in),
		Empty:    "no schedules yet",
		Rows:     len(w.listRows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return w.snapshot(in, pane) },
	}
}

func (w *Screen) help(in screen.Inputs) legend.Help {
	return w.actions(in).Help(w, w.deps.Keys)
}

// DoesRenderFromListing reports that the screen renders the listing's schedules.
func (w *Screen) DoesRenderFromListing() bool {
	return true
}

// TakesEveryKey reports whether a rename is taking the keys.
func (w *Screen) TakesEveryKey() bool {
	return w.IsRenaming()
}

// ActiveList is the list of schedules the filter narrows.
func (w *Screen) ActiveList() keys.KeyedList {
	return w.list()
}

// HoveredName is the name of the schedule under the cursor.
func (w *Screen) HoveredName(in screen.Inputs) string {
	return w.list().HoveredName(in)
}

// Column is the hovered schedule's own record rendered beside the list.
func (w *Screen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return w.docRows(in, width)
		},
	}
}

// StageResting restages the pane for the schedule the cursor rests on.
func (w *Screen) StageResting(screen.Inputs) screen.Request {
	return screen.Restage{}
}

// Dispatches is the screen's table and its archive's.
func (w *Screen) Dispatches() []keys.Table {
	return []keys.Table{w.actions(screen.Inputs{}), w.Archive().actions(screen.Inputs{})}
}
