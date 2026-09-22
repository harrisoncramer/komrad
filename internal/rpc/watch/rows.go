// Package watch serves the stream of row changes the panel holds open, and renders the rows the
// broker reads back for it.
package watch

import (
	"context"
	"fmt"
	"time"

	"weave/internal/agents"
	"weave/internal/rpc/render"
	"weave/internal/watch"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	agentstore "weave/internal/store/agent"
	markstore "weave/internal/store/mark"
	rowstore "weave/internal/store/row"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
)

// snapshotPage is how many rows one page of a snapshot reads.
const snapshotPage = 500

type agentRowReader interface {
	ListRows(ctx context.Context, page rowstore.Page) ([]agents.Row, error)
	ListLiveRowsByID(ctx context.Context, ids []string) ([]agents.Row, error)
}

type workflowReader interface {
	ListPage(ctx context.Context, limit int64, cursor *workflowstore.Cursor, archived bool) ([]workflowstore.Workflow, error)
	ListLiveByID(ctx context.Context, ids []string) ([]workflowstore.Workflow, error)
}

type scheduleReader interface {
	ListPage(ctx context.Context, limit int64, cursor *schedulestore.Cursor) ([]schedulestore.Schedule, error)
	ListLiveByID(ctx context.Context, ids []string) ([]schedulestore.Schedule, error)
}

type markReader interface {
	ListMarks(ctx context.Context) ([]markstore.Mark, error)
}

// NewRowsParams is what the row reader is built over.
type NewRowsParams struct {
	Agents    agentRowReader
	Workflows workflowReader
	Schedules scheduleReader
	Marks     markReader
	// Now is the clock a schedule's next run is worked out from, time.Now when unset.
	Now func() time.Time
}

// Rows renders the rows the broker names the way the list RPCs do, a tombstone for an id the
// listing no longer holds.
type Rows struct {
	agents    agentRowReader
	workflows workflowReader
	schedules scheduleReader
	marks     markReader
	now       func() time.Time
}

// NewRows builds the reader over the stores the three listings read.
func NewRows(params NewRowsParams) *Rows {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Rows{
		agents:    params.Agents,
		workflows: params.Workflows,
		schedules: params.Schedules,
		marks:     params.Marks,
		now:       now,
	}
}

// Read renders each touched id as its row, or as a tombstone when its listing no longer holds
// it.
func (r *Rows) Read(ctx context.Context, touched watch.Touched) ([]watch.Event, error) {
	var events []watch.Event

	if len(touched.Agents) > 0 {
		rows, err := r.agents.ListLiveRowsByID(ctx, touched.Agents)
		if err != nil {
			return nil, err
		}
		byID := xslice.KeyBy(rows, func(row agents.Row) string { return row.ID })
		events = append(events, settle(watch.KindAgent, touched.Agents, byID, renderAgentEvent)...)
	}

	if len(touched.Workflows) > 0 {
		rows, err := r.workflows.ListLiveByID(ctx, touched.Workflows)
		if err != nil {
			return nil, err
		}
		marks, err := r.marksByWorkflow(ctx)
		if err != nil {
			return nil, err
		}
		byID := xslice.KeyBy(rows, func(row workflowstore.Workflow) string { return row.ID })
		events = append(events, settle(watch.KindWorkflow, touched.Workflows, byID, func(row workflowstore.Workflow) watch.Event {
			return renderWorkflowEvent(row, marks[row.ID])
		})...)
	}

	if len(touched.Schedules) > 0 {
		rows, err := r.schedules.ListLiveByID(ctx, touched.Schedules)
		if err != nil {
			return nil, err
		}
		now := r.now()
		byID := xslice.KeyBy(rows, func(row schedulestore.Schedule) string { return row.ID })
		events = append(events, settle(watch.KindSchedule, touched.Schedules, byID, func(row schedulestore.Schedule) watch.Event {
			return renderScheduleEvent(row, now)
		})...)
	}

	return events, nil
}

// Snapshot renders every row the three listings hold.
func (r *Rows) Snapshot(ctx context.Context) (watch.Snapshot, error) {
	var snapshot watch.Snapshot

	var agentCursor *agentstore.Cursor
	for {
		rows, err := r.agents.ListRows(ctx, rowstore.Page{Limit: snapshotPage, Cursor: agentCursor})
		if err != nil {
			return watch.Snapshot{}, fmt.Errorf("failed to snapshot the agents: %w", err)
		}
		snapshot.Agents = append(snapshot.Agents, xslice.Map(rows, func(row agents.Row) *agentv1.Agent { return render.Agent(row, render.AgentOptions{}) })...)
		if len(rows) < snapshotPage {
			break
		}
		agentCursor = &agentstore.Cursor{ID: rows[len(rows)-1].ID}
	}

	marks, err := r.marksByWorkflow(ctx)
	if err != nil {
		return watch.Snapshot{}, err
	}
	var workflowCursor *workflowstore.Cursor
	for {
		rows, err := r.workflows.ListPage(ctx, snapshotPage, workflowCursor, false)
		if err != nil {
			return watch.Snapshot{}, fmt.Errorf("failed to snapshot the workflows: %w", err)
		}
		snapshot.Workflows = append(snapshot.Workflows, xslice.Map(rows, func(row workflowstore.Workflow) *workflowv1.Workflow {
			return render.Workflow(row, marks[row.ID], render.WorkflowOptions{})
		})...)
		if len(rows) < snapshotPage {
			break
		}
		last := rows[len(rows)-1]
		workflowCursor = &workflowstore.Cursor{Name: last.Name, ID: last.ID}
	}

	now := r.now()
	var scheduleCursor *schedulestore.Cursor
	for {
		rows, err := r.schedules.ListPage(ctx, snapshotPage, scheduleCursor)
		if err != nil {
			return watch.Snapshot{}, fmt.Errorf("failed to snapshot the schedules: %w", err)
		}
		snapshot.Schedules = append(snapshot.Schedules, xslice.Map(rows, func(row schedulestore.Schedule) *schedulev1.Schedule {
			return render.Schedule(row, now, render.ScheduleOptions{})
		})...)
		if len(rows) < snapshotPage {
			break
		}
		scheduleCursor = &schedulestore.Cursor{ID: rows[len(rows)-1].ID}
	}

	return snapshot, nil
}

func (r *Rows) marksByWorkflow(ctx context.Context) (map[string]*string, error) {
	marks, err := r.marks.ListMarks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the marks: %w", err)
	}

	byWorkflow := map[string]*string{}
	for _, mark := range marks {
		if mark.WorkflowID != nil {
			byWorkflow[*mark.WorkflowID] = &mark.Mark
		}
	}

	return byWorkflow, nil
}

// settle answers every touched id in order, with its row where the listing holds it and a
// tombstone where it does not.
func settle[R any](kind watch.Kind, ids []string, byID map[string]R, render func(R) watch.Event) []watch.Event {
	return xslice.Map(ids, func(id string) watch.Event {
		if row, ok := byID[id]; ok {
			return render(row)
		}

		return watch.Event{Kind: kind, ID: id}
	})
}

func renderAgentEvent(row agents.Row) watch.Event {
	return watch.Event{Kind: watch.KindAgent, ID: row.ID, Agent: render.Agent(row, render.AgentOptions{})}
}

func renderWorkflowEvent(row workflowstore.Workflow, mark *string) watch.Event {
	return watch.Event{Kind: watch.KindWorkflow, ID: row.ID, Workflow: render.Workflow(row, mark, render.WorkflowOptions{})}
}

func renderScheduleEvent(row schedulestore.Schedule, now time.Time) watch.Event {
	return watch.Event{Kind: watch.KindSchedule, ID: row.ID, Schedule: render.Schedule(row, now, render.ScheduleOptions{})}
}
