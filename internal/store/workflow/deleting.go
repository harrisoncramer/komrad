package workflow

import (
	"context"
	"fmt"

	"weave/internal/store"
)

// MarkDeleting claims a workflow for a delete, leaving a claim already there as it was.
func (s *Store) MarkDeleting(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkWorkflowDeleting(ctx, id); err != nil {
		return fmt.Errorf("failed to claim workflow %q for a delete: %w", id, err)
	}

	return nil
}

// MarkArchivedDeleting claims every archived workflow holding archived agents for a delete.
func (s *Store) MarkArchivedDeleting(ctx context.Context) error {
	if err := s.queries.MarkArchivedWorkflowsDeleting(ctx); err != nil {
		return fmt.Errorf("failed to claim the archived workflows for a delete: %w", err)
	}

	return nil
}

// ClearAllDeleting takes the delete's claim off every workflow.
func (s *Store) ClearAllDeleting(ctx context.Context) error {
	if err := s.queries.ClearAllWorkflowsDeleting(ctx); err != nil {
		return fmt.Errorf("failed to release every workflow from a delete: %w", err)
	}

	return nil
}
