package workflow

import (
	"context"
	"fmt"

	"weave/internal/store"
)

// Archive hides a workflow from the panel without dropping the row, leaving the agents inside
// it to the caller.
func (s *WorkflowStore) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkWorkflowArchived(ctx, id); err != nil {
		return fmt.Errorf("failed to archive workflow %q: %w", id, err)
	}

	s.db.TouchWorkflows(id)

	return nil
}

// Restore puts an archived workflow back in the panel, leaving its agents to the caller.
func (s *WorkflowStore) Restore(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkWorkflowRestored(ctx, id); err != nil {
		return fmt.Errorf("failed to restore workflow %q: %w", id, err)
	}

	s.db.TouchWorkflows(id)

	return nil
}
