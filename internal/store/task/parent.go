package task

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

// Parent is the task a child depends on, and the fork point where the child's own commits begin.
type Parent struct {
	// TaskID is the parent task.
	TaskID string
	// ForkPoint is the commit of the parent's branch the child's own commits sit on.
	ForkPoint string
	// PullRequestID is the parent's pull request, empty until the parent has one.
	PullRequestID string
	// Blocked is why the cascade cannot rebase the child, empty while nothing blocks it.
	Blocked CascadeBlock
}

// CascadeBlock is why the cascade cannot rebase a child onto its parent.
type CascadeBlock string

const (
	// CascadeConflicted is a rebase onto the parent that stopped on conflicts.
	CascadeConflicted CascadeBlock = "conflicts"
	// CascadeOrphaned is a child whose branch holds neither its fork point nor its parent's newest commit.
	CascadeOrphaned CascadeBlock = "orphaned"
	// CascadeParentClosed is a parent whose pull request closed without merging.
	CascadeParentClosed CascadeBlock = "closed"
)

func parentFrom(row *models.Task) *Parent {
	if row.ParentTaskID == nil {
		return nil
	}

	return &Parent{
		TaskID:        *row.ParentTaskID,
		ForkPoint:     xptr.Deref(row.ForkPoint),
		PullRequestID: xptr.Deref(row.ParentPullRequestID),
		Blocked:       CascadeBlock(xptr.Deref(row.CascadeBlocked)),
	}
}

// SetParent replaces a task's parent, or clears it with nil.
func (s *TaskStore) SetParent(ctx context.Context, id string, parent *Parent) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	params := models.SetTaskParentParams{ID: id}
	if parent != nil {
		params.ParentTaskID = &parent.TaskID
		params.ForkPoint = xptr.NonEmpty(parent.ForkPoint)
		params.ParentPullRequestID = xptr.NonEmpty(parent.PullRequestID)
	}

	if err := s.queries.SetTaskParent(ctx, params); err != nil {
		return fmt.Errorf("failed to set the parent of task %q: %w", id, err)
	}

	return nil
}

// SetForkPoint records a child's new fork point and clears what blocked its cascade.
func (s *TaskStore) SetForkPoint(ctx context.Context, id, forkPoint string) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetTaskForkPoint(ctx, models.SetTaskForkPointParams{ID: id, ForkPoint: &forkPoint}); err != nil {
		return fmt.Errorf("failed to set the fork point of task %q: %w", id, err)
	}

	return nil
}

// RecordParentPullRequest records a child's parent pull request, keeping one already recorded.
func (s *TaskStore) RecordParentPullRequest(ctx context.Context, id, pullRequestID string) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}
	if !store.IsID(pullRequestID) {
		return fmt.Errorf("%w: pull request %q", store.ErrNotFound, pullRequestID)
	}

	if err := s.queries.RecordTaskParentPullRequest(ctx, models.RecordTaskParentPullRequestParams{
		ID:                  id,
		ParentPullRequestID: &pullRequestID,
	}); err != nil {
		return fmt.Errorf("failed to record the parent pull request of task %q: %w", id, err)
	}

	return nil
}

// ListChildren returns every live child.
func (s *TaskStore) ListChildren(ctx context.Context) ([]Task, error) {
	rows, err := s.queries.ListChildTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the child tasks: %w", err)
	}

	return xslice.Map(rows, fromRow), nil
}

// ListChildrenOf returns the undeleted children of any of ids, archived or not.
func (s *TaskStore) ListChildrenOf(ctx context.Context, ids []string) ([]Task, error) {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListChildrenOfTasks(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the children of %d tasks: %w", len(storable), err)
	}

	return xslice.Map(rows, fromRow), nil
}

// SetCascadeBlocked records why the cascade cannot rebase a child, or clears it with an empty block.
func (s *TaskStore) SetCascadeBlocked(ctx context.Context, id string, block CascadeBlock) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetTaskCascadeBlocked(ctx, models.SetTaskCascadeBlockedParams{
		ID:             id,
		CascadeBlocked: xptr.NonEmpty(string(block)),
	}); err != nil {
		return fmt.Errorf("failed to set what blocks the cascade of task %q: %w", id, err)
	}

	return nil
}
