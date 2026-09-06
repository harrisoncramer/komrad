package workflow

import (
	"context"
	"fmt"

	"weave/internal/store"
)

// Archive hides a workflow from the panel without dropping the row, leaving the caller to
// unassign or archive the agents inside it in the same transaction.
func (s *Store) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkWorkflowArchived(ctx, id); err != nil {
		return fmt.Errorf("failed to archive workflow %q: %w", id, err)
	}

	return nil
}

// Restore puts an archived workflow back in the panel, leaving the caller to bring its agents
// back in the same transaction so it does not read as empty.
func (s *Store) Restore(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkWorkflowRestored(ctx, id); err != nil {
		return fmt.Errorf("failed to restore workflow %q: %w", id, err)
	}

	return nil
}
