package task

import (
	"context"
	"fmt"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"
	"komrad/internal/store"
)

// ErrDeleting is a task an agent was asked to join while it is being deleted.
var ErrDeleting = fail.Precondition("an agent cannot join a task being deleted")

// MarkDeleting claims tasks for a delete, leaving any already claimed as they were.
func (s *TaskStore) MarkDeleting(ctx context.Context, ids []string) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.MarkTasksDeleting(ctx, storable); err != nil {
		return fmt.Errorf("failed to claim %d tasks for a delete: %w", len(storable), err)
	}

	return nil
}

// MarkWorkflowDeleting claims every task in a workflow for a delete.
func (s *TaskStore) MarkWorkflowDeleting(ctx context.Context, workflowID string) error {
	if !store.IsID(workflowID) {
		return nil
	}

	if err := s.queries.MarkWorkflowTasksDeleting(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to claim the tasks of workflow %q for a delete: %w", workflowID, err)
	}

	return nil
}

// ClaimEmpty takes a task being deleted for the one cleanup that removes it, reporting false when
// an agent is still on it or another cleanup took it first.
func (s *TaskStore) ClaimEmpty(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	claimed, err := s.queries.ClaimEmptyDeletingTask(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to claim task %q for its cleanup: %w", id, err)
	}

	return claimed > 0, nil
}

// ClearDeleting takes the delete's claim off a task.
func (s *TaskStore) ClearDeleting(ctx context.Context, id string) error {
	if err := s.queries.ClearTaskDeleting(ctx, id); err != nil {
		return fmt.Errorf("failed to release task %q from its delete: %w", id, err)
	}

	return nil
}

// RecordCleanupFailure gives a task back from its delete with why its checkout could not be given back.
func (s *TaskStore) RecordCleanupFailure(ctx context.Context, id string, cause error) error {
	if err := s.queries.FailTaskCleanup(ctx, models.FailTaskCleanupParams{
		ID:            id,
		FailureReason: store.FailureReason(cause),
	}); err != nil {
		return fmt.Errorf("failed to record the failed cleanup of task %q: %w", id, err)
	}

	return nil
}

// ClearAbandonedDeleting takes the delete's claim off every task no agent delete still holds.
func (s *TaskStore) ClearAbandonedDeleting(ctx context.Context) error {
	if err := s.queries.ClearAbandonedTasksDeleting(ctx); err != nil {
		return fmt.Errorf("failed to release the tasks no delete holds: %w", err)
	}

	return nil
}

// ListIDsInWorkflow returns the id of every task in a workflow, archived and deleting ones included.
func (s *TaskStore) ListIDsInWorkflow(ctx context.Context, workflowID string) ([]string, error) {
	if !store.IsID(workflowID) {
		return nil, nil
	}

	ids, err := s.queries.ListWorkflowTaskIDs(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the tasks of workflow %q: %w", workflowID, err)
	}

	return ids, nil
}

// Delete permanently removes a task, taking its documents, reviews and pull request links with it.
func (s *TaskStore) Delete(ctx context.Context, id string) error {
	if err := s.queries.DeleteTask(ctx, id); err != nil {
		return fmt.Errorf("failed to delete task %q: %w", id, err)
	}

	return nil
}

// LockUndeleted holds a task against a delete's cleanup and any other create on it until the
// transaction ends, refusing one already being deleted.
func (s *TaskStore) LockUndeleted(ctx context.Context, id string) error {
	_, err := s.queries.LockUndeletedTask(ctx, id)
	if db.IsNoRows(err) {
		return ErrDeleting.Saying("task %q is being deleted", id)
	}
	if err != nil {
		return fmt.Errorf("failed to lock task %q: %w", id, err)
	}

	return nil
}

// GetUndeletedForUpdate locks an undeleted task until the transaction ends and returns it.
func (s *TaskStore) GetUndeletedForUpdate(ctx context.Context, id string) (*Task, error) {
	row, err := s.queries.GetUndeletedTaskForUpdate(ctx, id)
	if db.IsNoRows(err) {
		return nil, ErrDeleting.Saying("task %q is being deleted", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task %q for update: %w", id, err)
	}

	locked := fromRow(row)

	return &locked, nil
}

// ListUndeletedSlugs returns the slug of every task no delete has claimed, archived ones
// included, keyed by the task's id.
func (s *TaskStore) ListUndeletedSlugs(ctx context.Context) (map[string]string, error) {
	rows, err := s.queries.ListUndeletedTaskSlugs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the tasks not being deleted: %w", err)
	}

	slugs := make(map[string]string, len(rows))
	for _, row := range rows {
		slugs[row.ID] = row.Slug
	}

	return slugs, nil
}
