package workflow

import (
	"context"
	"fmt"
)

// ClearAllDeleting takes the delete's claim off every workflow.
func (s *WorkflowStore) ClearAllDeleting(ctx context.Context) error {
	released, err := s.queries.ClearAllWorkflowsDeleting(ctx)
	if err != nil {
		return fmt.Errorf("failed to release every workflow from a delete: %w", err)
	}

	s.db.TouchWorkflows(released...)

	return nil
}
