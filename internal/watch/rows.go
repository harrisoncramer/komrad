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

// Stores is the store each resource is read from.
type Stores struct {
	Agents       reader[rowstore.Row]
	Workflows    reader[workflowstore.Workflow]
	Schedules    reader[schedulestore.Schedule]
	Marks        reader[markstore.Mark]
	Mail         reader[mailstore.Mail]
	PullRequests reader[pullrequeststore.PullRequest]
}

// NewRowsParams is what the read-back is built over.
type NewRowsParams struct {
	Stores Stores
	// Now is the clock a schedule's next run is worked out from, time.Now when unset.
	Now func() time.Time
}

// Rows renders the rows a notification names the way the list RPCs do, a tombstone for a key
// the watched set no longer holds, with one loader per resource.
type Rows struct {
	loaders map[watchv1.Resource]loader
}

// NewRows builds the read-back over the stores the resources are read from.
func NewRows(params NewRowsParams) *Rows {
	stores := params.Stores

	return &Rows{loaders: map[watchv1.Resource]loader{
		watchv1.Resource_RESOURCE_AGENT: newLoader(stores.Agents, func(row rowstore.Row) (string, *watchv1.Row) {
			agent := render.Agent(row, chooseAgentOptions(row))

			return agent.GetId(), &watchv1.Row{Row: &watchv1.Row_Agent{Agent: agent}}
		}),
		watchv1.Resource_RESOURCE_WORKFLOW: newLoader(stores.Workflows, func(row workflowstore.Workflow) (string, *watchv1.Row) {
			workflow := render.Workflow(row, render.WorkflowOptions{})

			return workflow.GetId(), &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: workflow}}
		}),
		watchv1.Resource_RESOURCE_SCHEDULE: newLoader(stores.Schedules, func(row schedulestore.Schedule) (string, *watchv1.Row) {
			schedule := render.Schedule(row, params.now(), render.ScheduleOptions{})

			return schedule.GetId(), &watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: schedule}}
		}),
		watchv1.Resource_RESOURCE_MARK: newLoader(stores.Marks, func(row markstore.Mark) (string, *watchv1.Row) {
			mark := render.Mark(row)

			return mark.GetId(), &watchv1.Row{Row: &watchv1.Row_Mark{Mark: mark}}
		}),
		watchv1.Resource_RESOURCE_MAIL: newLoader(stores.Mail, func(row mailstore.Mail) (string, *watchv1.Row) {
			mail := render.Mail(row)

			return mail.GetId(), &watchv1.Row{Row: &watchv1.Row_Mail{Mail: mail}}
		}),
		watchv1.Resource_RESOURCE_PULL_REQUEST: newLoader(stores.PullRequests, func(row pullrequeststore.PullRequest) (string, *watchv1.Row) {
			pullRequest := render.PullRequest(row)

			return pullRequest.GetId(), &watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: pullRequest}}
		}),
	}}
}

// now is the clock a render reads.
func (p NewRowsParams) now() time.Time {
	if p.Now == nil {
		return time.Now()
	}

	return p.Now()
}

// loader reads one resource: the rows its watched set holds among the given keys, and every row
// it holds.
type loader struct {
	read     func(ctx context.Context, keys []string) (map[string]*watchv1.Row, error)
	snapshot func(ctx context.Context) ([]Change, error)
}

// newLoader is one resource's loader over its store and its renderer.
func newLoader[R any](store reader[R], draw func(R) (string, *watchv1.Row)) loader {
	return loader{
		read: func(ctx context.Context, keys []string) (map[string]*watchv1.Row, error) {
			rows, err := store.ListByKey(ctx, keys)
			if err != nil {
				return nil, err
			}

			held := make(map[string]*watchv1.Row, len(rows))
			for _, row := range rows {
				key, drawn := draw(row)
				held[key] = drawn
			}

			return held, nil
		},
		snapshot: func(ctx context.Context) ([]Change, error) {
			rows, err := store.List(ctx)
			if err != nil {
				return nil, err
			}

			return xslice.Map(rows, func(row R) Change {
				key, drawn := draw(row)

				return Change{Key: key, Row: drawn}
			}), nil
		},
	}
}

// Read renders each pending key as its row, or as a tombstone when its watched set no longer
// holds it, resource by resource.
func (r *Rows) Read(ctx context.Context, pending pendingKeys) ([]Change, error) {
	var changes []Change
	for _, res := range slices.Sorted(maps.Keys(pending)) {
		keys := pending[res]
		if len(keys) == 0 {
			continue
		}

		held, err := r.loaders[res].read(ctx, keys)
		if err != nil {
			return nil, err
		}

		changes = append(changes, xslice.Map(keys, func(key string) Change {
			return Change{
				Resource: res,
				Key:      key,
				Row:      held[key],
			}
		})...)
	}

	return changes, nil
}

// Snapshot renders every row the watched sets hold, one resource after another.
func (r *Rows) Snapshot(ctx context.Context) ([]Change, error) {
	var changes []Change
	for _, res := range slices.Sorted(maps.Keys(r.loaders)) {
		drawn, err := r.loaders[res].snapshot(ctx)
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

// chooseAgentOptions draws an archived agent with only the opening of its prompt.
func chooseAgentOptions(row rowstore.Row) render.AgentOptions {
	if row.ArchivedAt != nil {
		return render.AgentOptions{PromptHead: render.SummaryHead}
	}

	return render.AgentOptions{}
}
