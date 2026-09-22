// Package notify is the database side of the watch: the triggers that announce a moved row,
// and the connection that hears them.
package notify

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
)

// NotifyStore attaches the watch's triggers and listens on its channel.
type NotifyStore struct {
	queries *models.Queries
}

// NewNotifyStore builds the store over a database handle.
func NewNotifyStore(db store.DBTX) *NotifyStore {
	return &NotifyStore{queries: models.New(db)}
}

// Resource is a table whose own rows the watch carries, keyed by Key.
type Resource struct {
	Table string
	Key   string
}

// Projection is a table whose rows are rendered onto a parent, the parent's key held in Column.
type Projection struct {
	Table  string
	Parent string
	Column string
}

// Lookup is a table whose parents are the rows of Parent whose ParentMatch column equals this
// table's Match column, narrowed by Where when it is given.
type Lookup struct {
	Table       string
	Parent      string
	ParentKey   string
	ParentMatch string
	Match       string
	Where       *string
}

// EnsureResourceTrigger puts the notify trigger on a resource's own table, replacing one that
// is already there.
func (s *NotifyStore) EnsureResourceTrigger(ctx context.Context, resource Resource) error {
	err := s.queries.AttachWatchTrigger(ctx, models.AttachWatchTriggerParams{
		Target:    resource.Table,
		KeyColumn: resource.Key,
	})
	if err != nil {
		return fmt.Errorf("failed to attach the watch trigger to %s: %w", resource.Table, err)
	}

	return nil
}

// EnsureProjectionTrigger puts the projection trigger on a table that holds its parent's key.
func (s *NotifyStore) EnsureProjectionTrigger(ctx context.Context, projection Projection) error {
	err := s.queries.AttachProjectionTrigger(ctx, models.AttachProjectionTriggerParams{
		Target:       projection.Table,
		Parent:       projection.Parent,
		ParentColumn: projection.Column,
	})
	if err != nil {
		return fmt.Errorf("failed to attach the %s projection trigger to %s: %w", projection.Parent, projection.Table, err)
	}

	return nil
}

// EnsureLookupTrigger puts the projection trigger on a table whose parents are found by matching a
// column.
func (s *NotifyStore) EnsureLookupTrigger(ctx context.Context, lookup Lookup) error {
	err := s.queries.AttachProjectionLookupTrigger(ctx, models.AttachProjectionLookupTriggerParams{
		Target:      lookup.Table,
		Parent:      lookup.Parent,
		ParentKey:   lookup.ParentKey,
		ParentMatch: lookup.ParentMatch,
		Match:       lookup.Match,
		ExtraWhere:  lookup.Where,
	})
	if err != nil {
		return fmt.Errorf("failed to attach the %s lookup trigger to %s: %w", lookup.Parent, lookup.Table, err)
	}

	return nil
}

// OpenWatchChannel puts this store's connection on the watch channel, after which it is given
// notifications rather than used for queries.
func (s *NotifyStore) OpenWatchChannel(ctx context.Context) error {
	if err := s.queries.ListenToWatchChannel(ctx); err != nil {
		return fmt.Errorf("failed to listen on the watch channel: %w", err)
	}

	return nil
}
