package agent

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/lib/xslice"

	"github.com/google/uuid"
)

// Archive moves agents out of live listings, returning the pane each row held as it was
// archived rather than the one the caller last read.
func (s *AgentStore) Archive(ctx context.Context, ids []uuid.UUID) ([]AgentPane, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// The pane is read under the lock the archive takes, which a resume or a create can record one
	// inside of.
	archived, err := s.queries.MarkAgentsArchived(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to archive %d agents: %w", len(ids), err)
	}

	return xslice.Map(archived, func(row *models.MarkAgentsArchivedRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// Restore returns archived agents to live listings.
func (s *AgentStore) Restore(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	if err := s.queries.RestoreAgents(ctx, ids); err != nil {
		return fmt.Errorf("failed to restore %d agents: %w", len(ids), err)
	}

	return nil
}

// ArchiveWorkflowAgents archives every live agent in a workflow, returning the pane each row
// held as it was archived.
func (s *AgentStore) ArchiveWorkflowAgents(ctx context.Context, workflowID uuid.UUID) ([]AgentPane, error) {
	archived, err := s.queries.MarkWorkflowAgentsArchived(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to archive agents of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(archived, func(row *models.MarkWorkflowAgentsArchivedRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// RestoreWorkflowAgents restores the archived agents in a workflow, leaving reviewers and the
// agents on a task whose worktree was deleted archived.
func (s *AgentStore) RestoreWorkflowAgents(ctx context.Context, workflowID uuid.UUID) error {
	if err := s.queries.MarkWorkflowAgentsRestored(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to restore agents of workflow %q: %w", workflowID, err)
	}

	return nil
}
