package watch

import (
	"context"

	notifystore "weave/internal/store/notify"
)

// triggerEnsurer puts the watch's triggers on the tables it reads.
type triggerEnsurer interface {
	EnsureResourceTrigger(ctx context.Context, resource notifystore.Resource) error
	EnsureProjectionTrigger(ctx context.Context, projection notifystore.Projection) error
	EnsureLookupTrigger(ctx context.Context, lookup notifystore.Lookup) error
}

// ensureTriggers puts a trigger on every table a resource is the rows of and on every table
// projected onto one, so a write to any of them announces the row it moved.
func ensureTriggers(ctx context.Context, store triggerEnsurer) error {
	for _, r := range Resources() {
		resource := notifystore.Resource{
			Table: r.Table,
			Key:   r.Key,
		}
		if err := store.EnsureResourceTrigger(ctx, resource); err != nil {
			return err
		}

		for _, p := range r.Projects {
			if err := ensureProjection(ctx, store, r, p); err != nil {
				return err
			}
		}
	}

	return nil
}

// ensureProjection puts the trigger on one projected table, by the parent's key when the row
// holds it and by a match against the parent's rows when it does not.
func ensureProjection(ctx context.Context, store triggerEnsurer, r Resource, p Projection) error {
	if p.Column != "" {
		projection := notifystore.Projection{
			Table:  p.Table,
			Parent: r.Table,
			Column: p.Column,
		}

		return store.EnsureProjectionTrigger(ctx, projection)
	}

	lookup := notifystore.Lookup{
		Table:       p.Table,
		Parent:      r.Table,
		ParentKey:   r.Key,
		ParentMatch: p.ParentMatch,
		Match:       p.Match,
		Where:       narrowing(p.Where),
	}

	return store.EnsureLookupTrigger(ctx, lookup)
}

// narrowing is a lookup's extra condition, absent when it has none.
func narrowing(where string) *string {
	if where == "" {
		return nil
	}

	return &where
}
