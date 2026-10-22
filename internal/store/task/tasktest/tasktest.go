// Package tasktest makes the task every agent but an orchestrator must work on, for tests that
// write agents straight to the store.
package tasktest

import (
	"context"

	"komrad/internal/store"
	taskstore "komrad/internal/store/task"

	"github.com/google/uuid"
)

// Seed makes a task in the workflow under a slug no other task holds and returns its id.
func Seed(ctx context.Context, db store.DBTX, workflowID uuid.UUID) (*uuid.UUID, error) {
	id, err := store.NewID()
	if err != nil {
		return nil, err
	}

	created, err := taskstore.NewTaskStore(db).Create(ctx, taskstore.CreateParams{
		WorkflowID: workflowID,
		Slug:       "task-" + id.String(),
		Title:      "task",
	})
	if err != nil {
		return nil, err
	}

	return &created.ID, nil
}
