package workflow

import (
	"context"
	"fmt"
)

// ClearAbandonedDeleting takes the delete's claim off every workflow no agent delete still holds.
func (s *WorkflowStore) ClearAbandonedDeleting(ctx context.Context) error {
	if err := s.queries.ClearAbandonedWorkflowsDeleting(ctx); err != nil {
		return fmt.Errorf("failed to release the workflows no delete holds: %w", err)
	}

	return nil
}
