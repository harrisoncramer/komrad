package agent

import (
	"context"
	"fmt"
	"time"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

type ArchiveIfUnchangedParams struct {
	ID        string
	UpdatedAt time.Time
}

// ArchiveIfUnchanged archives one agent only while its row still reads as it was observed,
// so an archive decided from an older read cannot hide an agent a resume has brought back.
func (s *Store) ArchiveIfUnchanged(ctx context.Context, params ArchiveIfUnchangedParams) (bool, error) {
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
func (s *Store) Archive(ctx context.Context, ids []string) ([]AgentPane, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	// The pane is read under the lock the archive takes, since a resume or a create can
	// record one between the caller's read and this write.
	archived, err := s.queries.MarkAgentsArchived(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to archive %d agents: %w", len(storable), err)
	}

	return xslice.Map(archived, func(row *models.MarkAgentsArchivedRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// Restore returns archived agents to live listings.
func (s *Store) Restore(ctx context.Context, ids []string) error {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.RestoreAgents(ctx, storable); err != nil {
		return fmt.Errorf("failed to restore %d agents: %w", len(storable), err)
	}

	return nil
}

// ArchiveWorkflowAgents archives every live agent in a workflow, returning the pane each row
// held as it was archived, for the same reason Archive does.
func (s *Store) ArchiveWorkflowAgents(ctx context.Context, workflowID string) ([]AgentPane, error) {
	archived, err := s.queries.MarkWorkflowAgentsArchived(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to archive agents of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(archived, func(row *models.MarkWorkflowAgentsArchivedRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// RestoreWorkflowAgents restores every archived agent in a workflow.
func (s *Store) RestoreWorkflowAgents(ctx context.Context, workflowID string) error {
	if err := s.queries.MarkWorkflowAgentsRestored(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to restore agents of workflow %q: %w", workflowID, err)
	}

	return nil
}
