package workflow

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Archive hides a workflow from the panel without dropping the row, leaving the agents inside
// it to the caller.
func (s *WorkflowStore) Archive(ctx context.Context, id uuid.UUID) error {
	if err := s.queries.MarkWorkflowArchived(ctx, id); err != nil {
		return fmt.Errorf("failed to archive workflow %q: %w", id, err)
	}

	return nil
}

// Restore puts an archived workflow back in the panel, leaving its agents to the caller.
func (s *WorkflowStore) Restore(ctx context.Context, id uuid.UUID) error {
	if err := s.queries.MarkWorkflowRestored(ctx, id); err != nil {
		return fmt.Errorf("failed to restore workflow %q: %w", id, err)
	}

	return nil
}
