package watch

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"komrad/lib/xslice"
	watchv1 "komrad/proto/gen/watch/v1"
)

// watchedLister reads the rows a resource's watched set holds among the given keys, or every one of them when keys is nil.
type watchedLister[R any] interface {
	ListWatched(ctx context.Context, keys []string) ([]R, error)
}

// Loader reads and renders the rows of one resource.
type Loader struct {
	Resource watchv1.Resource
	read     func(ctx context.Context, keys []string) (map[string]*watchv1.Row, error)
	snapshot func(ctx context.Context) ([]Change, error)
}

// NewLoader builds the loader of a resource over its lister and the renderer that keys and draws each row.
func NewLoader[R any](resource watchv1.Resource, lister watchedLister[R], draw func(R) (string, *watchv1.Row)) Loader {
	return Loader{
		Resource: resource,
		read: func(ctx context.Context, keys []string) (map[string]*watchv1.Row, error) {
			rows, err := lister.ListWatched(ctx, keys)
			if err != nil {
				return nil, fmt.Errorf("failed to read %d keys of %s: %w", len(keys), resource, err)
			}

			held := make(map[string]*watchv1.Row, len(rows))
			for _, row := range rows {
				key, drawn := draw(row)
				held[key] = drawn
			}

			return held, nil
		},
		snapshot: func(ctx context.Context) ([]Change, error) {
			rows, err := lister.ListWatched(ctx, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to snapshot %s: %w", resource, err)
			}

			return xslice.Map(rows, func(row R) Change {
				key, drawn := draw(row)

				return Change{
					Resource: resource,
					Key:      key,
					Row:      drawn,
				}
			}), nil
		},
	}
}

// Rows renders the rows a notification names the way the list RPCs do, a tombstone for a key
// the watched set no longer holds, with one loader per resource.
type Rows struct {
	loaders map[watchv1.Resource]Loader
}

// NewRows builds the read-back over one loader per resource.
func NewRows(loaders []Loader) *Rows {
	return &Rows{loaders: xslice.KeyBy(loaders, func(loader Loader) watchv1.Resource { return loader.Resource })}
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
			return nil, err
		}

		changes = append(changes, drawn...)
	}

	return changes, nil
}

// hasLoader reports whether the rows read a resource.
func (r *Rows) hasLoader(resource watchv1.Resource) bool {
	if r == nil {
		return false
	}

	_, ok := r.loaders[resource]

	return ok
}
