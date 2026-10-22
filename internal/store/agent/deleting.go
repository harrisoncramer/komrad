package agent

import (
	"context"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/lib/xslice"

	"github.com/google/uuid"
)

// Claim is one agent a delete took, with its task and role and the stamp every agent that delete
// took shares.
type Claim struct {
	ID         uuid.UUID
	TaskID     *uuid.UUID
	Role       Role
	DeletingAt time.Time
}

// MarkDeleting claims agents for a delete, a writer taking the reviewers on its task with it, and
// any archived workflow whose last archived agent that claim takes, returning the agents it claimed.
func (s *AgentStore) MarkDeleting(ctx context.Context, ids []uuid.UUID) ([]Claim, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.MarkAgentsDeleting(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to claim %d agents for a delete: %w", len(ids), err)
	}

	return xslice.Map(rows, func(row *models.MarkAgentsDeletingRow) Claim {
		return newClaim(row.ID, row.TaskID, Role(row.Role), row.DeletingAt)
	}), nil
}

// MarkArchivedDeletingByID claims the archived agents among those named whose delete has not
// already failed, a writer taking the reviewers on its task with it, and any archived workflow left
// with nothing unclaimed, returning the agents it claimed.
func (s *AgentStore) MarkArchivedDeletingByID(ctx context.Context, ids []uuid.UUID) ([]Claim, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.MarkArchivedAgentsDeletingByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to claim %d archived agents for a delete: %w", len(ids), err)
	}

	return xslice.Map(rows, func(row *models.MarkArchivedAgentsDeletingByIDRow) Claim {
		return newClaim(row.ID, row.TaskID, Role(row.Role), row.DeletingAt)
	}), nil
}

// ListProvisionedIDsByTask returns the agents each named task was provisioned a worktree for,
// none for a task a live agent still leads.
func (s *AgentStore) ListProvisionedIDsByTask(ctx context.Context, taskIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}

	ids, err := s.queries.ListProvisionedAgentIDsByTask(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents provisioned for %d tasks: %w", len(taskIDs), err)
	}

	return ids, nil
}

// ClearDeleting takes the delete's claim off agents, a writer's reviewers, their tasks and their
// workflows.
func (s *AgentStore) ClearDeleting(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	if err := s.queries.ClearAgentsDeleting(ctx, ids); err != nil {
		return fmt.Errorf("failed to release %d agents from a delete: %w", len(ids), err)
	}

	return nil
}

// ListDeletingRoots returns every claimed agent but a reviewer whose task's writer the same delete took.
func (s *AgentStore) ListDeletingRoots(ctx context.Context) ([]Claim, error) {
	rows, err := s.queries.ListDeletingRoots(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents a delete has claimed: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListDeletingRootsRow) Claim {
		return newClaim(row.ID, row.TaskID, Role(row.Role), row.DeletingAt)
	}), nil
}

// ClaimRoots are the claimed agents other than a reviewer whose task's writer the same claim took,
// one per delete.
func ClaimRoots(claims []Claim) []Claim {
	writerClaimed := make(map[uuid.UUID]bool, len(claims))
	for _, claim := range claims {
		if claim.Role == RoleWriter && claim.TaskID != nil {
			writerClaimed[*claim.TaskID] = true
		}
	}

	return xslice.Filter(claims, func(claim Claim) bool {
		return claim.Role != RoleReviewer || claim.TaskID == nil || !writerClaimed[*claim.TaskID]
	})
}

func newClaim(id uuid.UUID, taskID *uuid.UUID, role Role, deletingAt *time.Time) Claim {
	claim := Claim{
		ID:     id,
		TaskID: taskID,
		Role:   role,
	}
	if deletingAt != nil {
		claim.DeletingAt = *deletingAt
	}

	return claim
}
