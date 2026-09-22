// Package watch serves the stream of row changes the panel holds open, and renders the rows the
// broker reads back for it.
package watch

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"weave/internal/rpc/render"
	"weave/internal/watch"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	rowstore "weave/internal/store/row"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
)

// snapshotPage is how many rows one page of a snapshot reads.
const snapshotPage = 500

type agentRowReader interface {
	ListRows(ctx context.Context, page rowstore.Page) ([]rowstore.Row, error)
	ListLiveRowsByID(ctx context.Context, ids []string) ([]rowstore.Row, error)
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
	List(ctx context.Context) ([]markstore.Mark, error)
	ListByKey(ctx context.Context, marks []string) ([]markstore.Mark, error)
}

type mailReader interface {
	ListUnclaimed(ctx context.Context) ([]mailstore.Mail, error)
	ListByKey(ctx context.Context, ids []string) ([]mailstore.Mail, error)
}

type pullRequestReader interface {
	ListOnLiveBranches(ctx context.Context) ([]pullrequeststore.PullRequest, error)
	ListByKey(ctx context.Context, ids []string) ([]pullrequeststore.PullRequest, error)
}

// NewRowsParams is what the row reader is built over.
type NewRowsParams struct {
	Agents       agentRowReader
	Workflows    workflowReader
	Schedules    scheduleReader
	Marks        markReader
	Mail         mailReader
	PullRequests pullRequestReader
	// Now is the clock a schedule's next run is worked out from, time.Now when unset.
	Now func() time.Time
}

// Rows renders the rows the broker names the way the list RPCs do, a tombstone for a key the
// watched set no longer holds, with one arm per resource.
type Rows struct {
	arms map[watch.Kind]arm
}

// NewRows builds the reader over the stores the six resources are read from.
func NewRows(params NewRowsParams) *Rows {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Rows{arms: map[watch.Kind]arm{
		watch.KindAgent: newArm(
			params.Agents.ListLiveRowsByID,
			paged(func(ctx context.Context, cursor *agentstore.Cursor) ([]rowstore.Row, error) {
				return params.Agents.ListRows(ctx, rowstore.Page{Limit: snapshotPage, Cursor: cursor})
			}, func(row rowstore.Row) *agentstore.Cursor { return &agentstore.Cursor{ID: row.ID} }),
			func(row rowstore.Row) string { return row.ID },
			func(row rowstore.Row) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: render.Agent(row, render.AgentOptions{})}}
			},
		),
		watch.KindWorkflow: newArm(
			params.Workflows.ListLiveByID,
			paged(func(ctx context.Context, cursor *workflowstore.Cursor) ([]workflowstore.Workflow, error) {
				return params.Workflows.ListPage(ctx, snapshotPage, cursor, false)
			}, func(row workflowstore.Workflow) *workflowstore.Cursor {
				return &workflowstore.Cursor{Name: row.Name, ID: row.ID}
			}),
			func(row workflowstore.Workflow) string { return row.ID },
			func(row workflowstore.Workflow) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: render.Workflow(row, render.WorkflowOptions{})}}
			},
		),
		watch.KindSchedule: newArm(
			params.Schedules.ListLiveByID,
			paged(func(ctx context.Context, cursor *schedulestore.Cursor) ([]schedulestore.Schedule, error) {
				return params.Schedules.ListPage(ctx, snapshotPage, cursor)
			}, func(row schedulestore.Schedule) *schedulestore.Cursor { return &schedulestore.Cursor{ID: row.ID} }),
			func(row schedulestore.Schedule) string { return row.ID },
			func(row schedulestore.Schedule) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: render.Schedule(row, now(), render.ScheduleOptions{})}}
			},
		),
		watch.KindMark: newArm(
			params.Marks.ListByKey,
			params.Marks.List,
			func(row markstore.Mark) string { return row.Mark },
			func(row markstore.Mark) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Mark{Mark: render.Mark(row)}}
			},
		),
		watch.KindMail: newArm(
			params.Mail.ListByKey,
			params.Mail.ListUnclaimed,
			func(row mailstore.Mail) string { return row.ID },
			func(row mailstore.Mail) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Mail{Mail: render.Mail(row)}}
			},
		),
		watch.KindPullRequest: newArm(
			params.PullRequests.ListByKey,
			params.PullRequests.ListOnLiveBranches,
			func(row pullrequeststore.PullRequest) string { return row.ID },
			func(row pullrequeststore.PullRequest) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: render.PullRequest(row)}}
			},
		),
	}}
}

// arm reads one resource: the rows its watched set holds among the given keys, and every row
// it holds.
type arm struct {
	read     func(ctx context.Context, keys []string) (map[string]*watchv1.Row, error)
	snapshot func(ctx context.Context) ([]*watchv1.Row, error)
}

// newArm is one resource's arm over its by-key read, its whole read, its key and its renderer.
func newArm[R any](
	byKey func(ctx context.Context, keys []string) ([]R, error),
	all func(ctx context.Context) ([]R, error),
	key func(R) string,
	draw func(R) *watchv1.Row,
) arm {
	return arm{
		read: func(ctx context.Context, keys []string) (map[string]*watchv1.Row, error) {
			rows, err := byKey(ctx, keys)
			if err != nil {
				return nil, err
			}

			held := make(map[string]*watchv1.Row, len(rows))
			for _, row := range rows {
				held[key(row)] = draw(row)
			}

			return held, nil
		},
		snapshot: func(ctx context.Context) ([]*watchv1.Row, error) {
			rows, err := all(ctx)
			if err != nil {
				return nil, err
			}

			return xslice.Map(rows, draw), nil
		},
	}
}

// paged walks a keyset listing to its end, a snapshot page at a time.
func paged[R, C any](page func(ctx context.Context, cursor *C) ([]R, error), after func(R) *C) func(ctx context.Context) ([]R, error) {
	return func(ctx context.Context) ([]R, error) {
		var all []R
		var cursor *C
		for {
			rows, err := page(ctx, cursor)
			if err != nil {
				return nil, err
			}
			all = append(all, rows...)
			if len(rows) < snapshotPage {
				return all, nil
			}
			cursor = after(rows[len(rows)-1])
		}
	}
}

// Read renders each touched key as its row, or as a tombstone when its watched set no longer
// holds it, kind by kind.
func (r *Rows) Read(ctx context.Context, touched watch.Touched) ([]watch.Event, error) {
	var events []watch.Event
	for _, kind := range slices.Sorted(maps.Keys(touched)) {
		keys := touched[kind]
		if len(keys) == 0 {
			continue
		}

		held, err := r.arms[kind].read(ctx, keys)
		if err != nil {
			return nil, err
		}

		events = append(events, xslice.Map(keys, func(key string) watch.Event {
			return watch.Event{Kind: kind, Key: key, Row: held[key]}
		})...)
	}

	return events, nil
}

// Snapshot renders every row the six watched sets hold, one resource after another.
func (r *Rows) Snapshot(ctx context.Context) ([]*watchv1.Row, error) {
	var rows []*watchv1.Row
	for _, kind := range slices.Sorted(maps.Keys(r.arms)) {
		drawn, err := r.arms[kind].snapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to snapshot %s: %w", kind, err)
		}
		rows = append(rows, drawn...)
	}

	return rows, nil
}
