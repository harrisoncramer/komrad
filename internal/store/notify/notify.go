// Package notify opens the database connection that hears watch notifications.
package notify

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
)

// NotifyStore listens on the watch channel.
type NotifyStore struct {
	queries *models.Queries
}

// NewNotifyStore builds the store over a database handle.
func NewNotifyStore(db store.DBTX) *NotifyStore {
	return &NotifyStore{queries: models.New(db)}
}

// OpenWatchChannel puts this store's connection on the watch channel, after which it is given
// notifications rather than used for queries.
func (s *NotifyStore) OpenWatchChannel(ctx context.Context) error {
	if err := s.queries.ListenToWatchChannel(ctx); err != nil {
		return fmt.Errorf("failed to listen on the watch channel: %w", err)
	}

	return nil
}
