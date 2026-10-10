package task

import (
	"context"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/lib/xslice"
)

// Summary is what a worktree listing needs of a task to say how its checkout goes.
type Summary struct {
	ID         string
	WorkflowID string
	Slug       string
	// Adopted says the checkout was put on a branch komrad did not make.
	Adopted    bool
	ArchivedAt *time.Time
	Deleting   bool
}

// ListSummaries returns every task, archived and deleting ones included.
func (s *TaskStore) ListSummaries(ctx context.Context) ([]Summary, error) {
	rows, err := s.queries.ListTaskSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list task summaries: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListTaskSummariesRow) Summary {
		return Summary{
			ID:         row.ID,
			WorkflowID: row.WorkflowID,
			Slug:       row.Slug,
			Adopted:    row.AdoptedBranch != nil,
			ArchivedAt: row.ArchivedAt,
			Deleting:   row.DeletingAt != nil,
		}
	}), nil
}
