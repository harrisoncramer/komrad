package watch

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"weave/internal/rpc/render"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"

	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	rowstore "weave/internal/store/row"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
)

// reader is one resource's store: every row its watched set holds, and the ones among the
// given keys.
type reader[R any] interface {
	List(ctx context.Context) ([]R, error)
	ListByKey(ctx context.Context, keys []string) ([]R, error)
}

// NewRowsParams is what the row reader is built over.
type NewRowsParams struct {
	Agents       reader[rowstore.Row]
	Workflows    reader[workflowstore.Workflow]
	Schedules    reader[schedulestore.Schedule]
	Marks        reader[markstore.Mark]
	Mail         reader[mailstore.Mail]
	PullRequests reader[pullrequeststore.PullRequest]
	// Now is the clock a schedule's next run is worked out from, time.Now when unset.
	Now func() time.Time
}

// Rows renders the rows a notification names the way the list RPCs do, a tombstone for a key
// the watched set no longer holds, with one arm per resource.
type Rows struct {
	arms map[watchv1.Resource]arm
}

// NewRows builds the reader over the stores the six resources are read from.
func NewRows(params NewRowsParams) *Rows {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Rows{arms: map[watchv1.Resource]arm{
		watchv1.Resource_RESOURCE_AGENT: newArm(
			params.Agents,
			func(row rowstore.Row) string { return row.ID },
			func(row rowstore.Row) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: render.Agent(row, render.AgentOptions{})}}
			},
		),
		watchv1.Resource_RESOURCE_WORKFLOW: newArm(
			params.Workflows,
			func(row workflowstore.Workflow) string { return row.ID },
			func(row workflowstore.Workflow) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: render.Workflow(row, render.WorkflowOptions{})}}
			},
		),
		watchv1.Resource_RESOURCE_SCHEDULE: newArm(
			params.Schedules,
			func(row schedulestore.Schedule) string { return row.ID },
			func(row schedulestore.Schedule) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: render.Schedule(row, now(), render.ScheduleOptions{})}}
			},
		),
		watchv1.Resource_RESOURCE_MARK: newArm(
			params.Marks,
			func(row markstore.Mark) string { return row.Mark },
			func(row markstore.Mark) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Mark{Mark: render.Mark(row)}}
			},
		),
		watchv1.Resource_RESOURCE_MAIL: newArm(
			params.Mail,
			func(row mailstore.Mail) string { return row.ID },
			func(row mailstore.Mail) *watchv1.Row {
				return &watchv1.Row{Row: &watchv1.Row_Mail{Mail: render.Mail(row)}}
			},
		),
		watchv1.Resource_RESOURCE_PULL_REQUEST: newArm(
			params.PullRequests,
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
	snapshot func(ctx context.Context) ([]Change, error)
}

// newArm is one resource's arm over its store, its key and its renderer.
func newArm[R any](store reader[R], key func(R) string, draw func(R) *watchv1.Row) arm {
	return arm{
		read: func(ctx context.Context, keys []string) (map[string]*watchv1.Row, error) {
			rows, err := store.ListByKey(ctx, keys)
			if err != nil {
				return nil, err
			}

			held := make(map[string]*watchv1.Row, len(rows))
			for _, row := range rows {
				held[key(row)] = draw(row)
			}

			return held, nil
		},
		snapshot: func(ctx context.Context) ([]Change, error) {
			rows, err := store.List(ctx)
			if err != nil {
				return nil, err
			}

			return xslice.Map(rows, func(row R) Change {
				return Change{Key: key(row), Row: draw(row)}
			}), nil
		},
	}
}

// Read renders each touched key as its row, or as a tombstone when its watched set no longer
// holds it, resource by resource.
func (r *Rows) Read(ctx context.Context, touched Touched) ([]Change, error) {
	var changes []Change
	for _, res := range slices.Sorted(maps.Keys(touched)) {
		keys := touched[res]
		if len(keys) == 0 {
			continue
		}

		held, err := r.arms[res].read(ctx, keys)
		if err != nil {
			return nil, err
		}

		changes = append(changes, xslice.Map(keys, func(key string) Change {
			return Change{Resource: res, Key: key, Row: held[key]}
		})...)
	}

	return changes, nil
}

// Snapshot renders every row the six watched sets hold, one resource after another.
func (r *Rows) Snapshot(ctx context.Context) ([]Change, error) {
	var changes []Change
	for _, res := range slices.Sorted(maps.Keys(r.arms)) {
		drawn, err := r.arms[res].snapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to snapshot %s: %w", res, err)
		}

		for _, change := range drawn {
			change.Resource = res
			changes = append(changes, change)
		}
	}

	return changes, nil
}
