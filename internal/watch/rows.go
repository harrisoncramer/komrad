package watch

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

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
// the watched set no longer holds, with one arm per resource.
type Rows struct {
	arms map[watchv1.Resource]arm
}

// NewRows builds the read-back over the stores the resources are read from.
func NewRows(params NewRowsParams) *Rows {
	arms := map[watchv1.Resource]arm{}
	for _, r := range Resources() {
		arms[r.Enum] = r.bind(params.Stores, params.now)
	}

	return &Rows{arms: arms}
}

// now is the clock a render reads.
func (p NewRowsParams) now() time.Time {
	if p.Now == nil {
		return time.Now()
	}

	return p.Now()
}

// arm reads one resource: the rows its watched set holds among the given keys, and every row
// it holds.
type arm struct {
	read     func(ctx context.Context, keys []string) (map[string]*watchv1.Row, error)
	snapshot func(ctx context.Context) ([]Change, error)
}

// newArm is one resource's arm over its store and its renderer.
func newArm[R any](store reader[R], draw func(R) (string, *watchv1.Row)) arm {
	return arm{
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
