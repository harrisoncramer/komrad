package agent

import (
	"context"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// Claim is one agent a delete took, with the parent it hangs under and the stamp every agent
// that delete took shares.
type Claim struct {
	ID            string
	ParentAgentID *string
	DeletingAt    time.Time
}

// MarkDeleting claims agents and their children for a delete, and with them any archived
// workflow whose last archived agent that claim takes, returning the agents it claimed.
func (s *AgentStore) MarkDeleting(ctx context.Context, ids []string) ([]Claim, error) {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.MarkAgentsDeleting(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to claim %d agents for a delete: %w", len(storable), err)
	}

	return xslice.Map(rows, func(row *models.MarkAgentsDeletingRow) Claim {
		return newClaim(row.ID, row.ParentAgentID, row.DeletingAt)
	}), nil
}

// MarkArchivedDeletingByID claims the archived agents among those named whose delete has not
// already failed, their children, and any archived workflow left with nothing unclaimed,
// returning the agents it claimed.
func (s *AgentStore) MarkArchivedDeletingByID(ctx context.Context, ids []string) ([]Claim, error) {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.MarkArchivedAgentsDeletingByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to claim %d archived agents for a delete: %w", len(storable), err)
	}

	return xslice.Map(rows, func(row *models.MarkArchivedAgentsDeletingByIDRow) Claim {
		return newClaim(row.ID, row.ParentAgentID, row.DeletingAt)
	}), nil
}

// ClearDeleting takes the delete's claim off agents, their children and their workflows.
func (s *AgentStore) ClearDeleting(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.ClearAgentsDeleting(ctx, storable); err != nil {
		return fmt.Errorf("failed to release %d agents from a delete: %w", len(storable), err)
	}

	return nil
}

// ListDeletingRoots returns every claimed agent whose parent the same delete did not take.
func (s *AgentStore) ListDeletingRoots(ctx context.Context) ([]Claim, error) {
	rows, err := s.queries.ListDeletingRoots(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents a delete has claimed: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListDeletingRootsRow) Claim {
		return newClaim(row.ID, row.ParentAgentID, row.DeletingAt)
	}), nil
}

// ClaimRoots are the claimed agents whose parent the same claim did not take, one per delete.
func ClaimRoots(claims []Claim) []Claim {
	claimed := make(map[string]bool, len(claims))
	for _, claim := range claims {
		claimed[claim.ID] = true
	}

	return xslice.Filter(claims, func(claim Claim) bool {
		return claim.ParentAgentID == nil || !claimed[*claim.ParentAgentID]
	})
}

func newClaim(id string, parentAgentID *string, deletingAt *time.Time) Claim {
	claim := Claim{ID: id, ParentAgentID: parentAgentID}
	if deletingAt != nil {
		claim.DeletingAt = *deletingAt
	}

	return claim
}
