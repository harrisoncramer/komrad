package agent

import (
	"context"
	"fmt"

	"weave/internal/store"
)

// MarkDeleting claims agents and their children for a delete, and with them any archived
// workflow whose last archived agent that claim takes.
func (s *Store) MarkDeleting(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.MarkAgentsDeleting(ctx, storable); err != nil {
		return fmt.Errorf("failed to claim %d agents for a delete: %w", len(storable), err)
	}

	return nil
}

// MarkArchivedDeleting claims every archived agent whose delete has not already failed, and
// every archived workflow left with nothing unclaimed inside it.
func (s *Store) MarkArchivedDeleting(ctx context.Context) error {
	if err := s.queries.MarkArchivedAgentsDeleting(ctx); err != nil {
		return fmt.Errorf("failed to claim the archive for a delete: %w", err)
	}

	return nil
}

// MarkArchivedDeletingByID claims the archived agents among those named whose delete has not
// already failed, their children, and any archived workflow left with nothing unclaimed.
func (s *Store) MarkArchivedDeletingByID(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.MarkArchivedAgentsDeletingByID(ctx, storable); err != nil {
		return fmt.Errorf("failed to claim %d archived agents for a delete: %w", len(storable), err)
	}

	return nil
}

// ClearDeleting takes the delete's claim off agents, their children and their workflows.
func (s *Store) ClearDeleting(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.ClearAgentsDeleting(ctx, storable); err != nil {
		return fmt.Errorf("failed to release %d agents from a delete: %w", len(storable), err)
	}

	return nil
}

// ClearAllDeleting takes the delete's claim off every agent.
func (s *Store) ClearAllDeleting(ctx context.Context) error {
	if err := s.queries.ClearAllAgentsDeleting(ctx); err != nil {
		return fmt.Errorf("failed to release every agent from a delete: %w", err)
	}

	return nil
}
