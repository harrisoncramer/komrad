package agent

import (
	"context"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

type ArchiveIfUnchangedParams struct {
	ID        string
	UpdatedAt time.Time
}

// ArchiveIfUnchanged archives one agent only while its row still reads as it was observed.
func (s *AgentStore) ArchiveIfUnchanged(ctx context.Context, params ArchiveIfUnchangedParams) (bool, error) {
	if !store.IsID(params.ID) {
		return false, nil
	}

	archived, err := s.queries.MarkAgentArchivedIfUnchanged(ctx, models.MarkAgentArchivedIfUnchangedParams{
		ID:        params.ID,
		UpdatedAt: params.UpdatedAt,
	})
	if err != nil {
		return false, fmt.Errorf("failed to archive id %q: %w", params.ID, err)
	}

	return len(archived) > 0, nil
}

// Archive moves agents out of live listings, returning the pane each row held as it was
// archived rather than the one the caller last read.
func (s *AgentStore) Archive(ctx context.Context, ids []string) ([]AgentPane, error) {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	// The pane is read under the lock the archive takes, which a resume or a create can record one
	// inside of.
	archived, err := s.queries.MarkAgentsArchived(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to archive %d agents: %w", len(storable), err)
	}

	return xslice.Map(archived, func(row *models.MarkAgentsArchivedRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// RestampArchived moves the stamp of agents that are already archived to now.
func (s *AgentStore) RestampArchived(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.RestampAgentsArchived(ctx, storable); err != nil {
		return fmt.Errorf("failed to restamp %d archived agents: %w", len(storable), err)
	}

	return nil
}

// Restore returns archived agents to live listings.
func (s *AgentStore) Restore(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.RestoreAgents(ctx, storable); err != nil {
		return fmt.Errorf("failed to restore %d agents: %w", len(storable), err)
	}

	return nil
}

// ArchiveWorkflowAgents archives every live agent in a workflow, returning the pane each row
// held as it was archived.
func (s *AgentStore) ArchiveWorkflowAgents(ctx context.Context, workflowID string) ([]AgentPane, error) {
	archived, err := s.queries.MarkWorkflowAgentsArchived(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to archive agents of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(archived, func(row *models.MarkWorkflowAgentsArchivedRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// RestoreWorkflowAgents restores every archived agent in a workflow.
func (s *AgentStore) RestoreWorkflowAgents(ctx context.Context, workflowID string) error {
	if err := s.queries.MarkWorkflowAgentsRestored(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to restore agents of workflow %q: %w", workflowID, err)
	}

	return nil
}
