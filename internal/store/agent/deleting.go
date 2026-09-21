package agent

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

// claimKindWorkflow is what a delete claim's row says when it names a workflow rather than an
// agent.
const claimKindWorkflow = "workflow"

// claimed is one row a delete claim changed, an agent or a workflow.
type claimed struct {
	ID   string
	Kind string
}

// touchClaimed says which agents and workflows a delete claim moved.
func (s *AgentStore) touchClaimed(rows []claimed) {
	var agents, workflows []string
	for _, row := range rows {
		if row.Kind == claimKindWorkflow {
			workflows = append(workflows, row.ID)

			continue
		}
		agents = append(agents, row.ID)
	}

	s.db.TouchAgents(agents...)
	s.db.TouchWorkflows(workflows...)
}

// MarkDeleting claims agents and their children for a delete, and with them any archived
// workflow whose last archived agent that claim takes.
func (s *AgentStore) MarkDeleting(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	rows, err := s.queries.MarkAgentsDeleting(ctx, storable)
	if err != nil {
		return fmt.Errorf("failed to claim %d agents for a delete: %w", len(storable), err)
	}

	s.touchClaimed(xslice.Map(rows, func(row *models.MarkAgentsDeletingRow) claimed { return claimed(*row) }))

	return nil
}

// MarkArchivedDeleting claims every archived agent whose delete has not already failed, and
// every archived workflow left with nothing unclaimed inside it.
func (s *AgentStore) MarkArchivedDeleting(ctx context.Context) error {
	rows, err := s.queries.MarkArchivedAgentsDeleting(ctx)
	if err != nil {
		return fmt.Errorf("failed to claim the archive for a delete: %w", err)
	}

	s.touchClaimed(xslice.Map(rows, func(row *models.MarkArchivedAgentsDeletingRow) claimed { return claimed(*row) }))

	return nil
}

// MarkArchivedDeletingByID claims the archived agents among those named whose delete has not
// already failed, their children, and any archived workflow left with nothing unclaimed.
func (s *AgentStore) MarkArchivedDeletingByID(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	rows, err := s.queries.MarkArchivedAgentsDeletingByID(ctx, storable)
	if err != nil {
		return fmt.Errorf("failed to claim %d archived agents for a delete: %w", len(storable), err)
	}

	s.touchClaimed(xslice.Map(rows, func(row *models.MarkArchivedAgentsDeletingByIDRow) claimed { return claimed(*row) }))

	return nil
}

// ClearDeleting takes the delete's claim off agents, their children and their workflows.
func (s *AgentStore) ClearDeleting(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	rows, err := s.queries.ClearAgentsDeleting(ctx, storable)
	if err != nil {
		return fmt.Errorf("failed to release %d agents from a delete: %w", len(storable), err)
	}

	s.touchClaimed(xslice.Map(rows, func(row *models.ClearAgentsDeletingRow) claimed { return claimed(*row) }))

	return nil
}

// ClearAllDeleting takes the delete's claim off every agent.
func (s *AgentStore) ClearAllDeleting(ctx context.Context) error {
	released, err := s.queries.ClearAllAgentsDeleting(ctx)
	if err != nil {
		return fmt.Errorf("failed to release every agent from a delete: %w", err)
	}

	s.db.TouchAgents(released...)

	return nil
}
