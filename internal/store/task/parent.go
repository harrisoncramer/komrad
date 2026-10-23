package task

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/internal/fail"
	"komrad/lib/xptr"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/google/uuid"
)

// Parent is the task a child depends on, and the fork point where the child's own commits begin.
type Parent struct {
	// TaskID is the parent task.
	TaskID uuid.UUID
	// ForkPoint is the commit of the parent's branch the child's own commits sit on.
	ForkPoint string
	// PullRequestID is the parent's pull request, empty until the parent has one.
	PullRequestID *uuid.UUID
	// Blocked is why the cascade cannot rebase the child, unspecified while nothing blocks it.
	Blocked taskv1.CascadeBlock
}

// errNoParent is a task row that depends on no other task.
var errNoParent = fail.NotFound("the task has no parent")

func parentFrom(row *models.Task) (*Parent, error) {
	if row.ParentTaskID == nil {
		return nil, errNoParent
	}
	blocked, err := fromDBToServiceCascadeBlock(row.CascadeBlocked)
	if err != nil {
		return nil, err
	}

	return &Parent{
		TaskID:        *row.ParentTaskID,
		ForkPoint:     xptr.Deref(row.ForkPoint),
		PullRequestID: row.ParentPullRequestID,
		Blocked:       blocked,
	}, nil
}

// SetParent replaces a task's parent, or clears it with nil.
func (s *TaskStore) SetParent(ctx context.Context, id uuid.UUID, parent *Parent) error {
	params := models.SetTaskParentParams{ID: id}
	if parent != nil {
		params.ParentTaskID = &parent.TaskID
		params.ForkPoint = xptr.NonEmpty(parent.ForkPoint)
		params.ParentPullRequestID = parent.PullRequestID
	}

	if err := s.queries.SetTaskParent(ctx, params); err != nil {
		return fmt.Errorf("failed to set the parent of task %q: %w", id, err)
	}

	return nil
}

// ClearParent clears the child's parent and records the parent's branch its pull request moves off.
func (s *TaskStore) ClearParent(ctx context.Context, id uuid.UUID, branch string) error {
	if err := s.queries.ClearTaskParent(ctx, models.ClearTaskParentParams{ID: id, LeftParentBranch: xptr.NonEmpty(branch)}); err != nil {
		return fmt.Errorf("failed to clear the parent of task %q: %w", id, err)
	}

	return nil
}

// ClearLeftParentBranch records that the task's pull request no longer waits to move off its parent's branch.
func (s *TaskStore) ClearLeftParentBranch(ctx context.Context, id uuid.UUID) error {
	if err := s.queries.ClearTaskLeftParentBranch(ctx, id); err != nil {
		return fmt.Errorf("failed to clear the branch task %q left: %w", id, err)
	}

	return nil
}

// SetForkPoint records a child's new fork point and clears what blocked its cascade.
func (s *TaskStore) SetForkPoint(ctx context.Context, id uuid.UUID, forkPoint string) error {
	if err := s.queries.SetTaskForkPoint(ctx, models.SetTaskForkPointParams{ID: id, ForkPoint: &forkPoint}); err != nil {
		return fmt.Errorf("failed to set the fork point of task %q: %w", id, err)
	}

	return nil
}

// RecordParentPullRequest records a child's parent pull request, keeping one already recorded.
func (s *TaskStore) RecordParentPullRequest(ctx context.Context, id, pullRequestID uuid.UUID) error {
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

	return fromRows(rows)
}

// ListChildrenOf returns the undeleted children of any of ids, archived or not.
func (s *TaskStore) ListChildrenOf(ctx context.Context, ids []uuid.UUID) ([]Task, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListChildrenOfTasks(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the children of %d tasks: %w", len(ids), err)
	}

	return fromRows(rows)
}

// SetCascadeBlocked records why the cascade cannot rebase a child, or clears it with an unspecified block.
func (s *TaskStore) SetCascadeBlocked(ctx context.Context, id uuid.UUID, block taskv1.CascadeBlock) error {
	var stored *string
	if block != taskv1.CascadeBlock_CASCADE_BLOCK_UNSPECIFIED {
		text, err := fromServiceToDBCascadeBlock(block)
		if err != nil {
			return err
		}
		stored = &text
	}

	if err := s.queries.SetTaskCascadeBlocked(ctx, models.SetTaskCascadeBlockedParams{
		ID:             id,
		CascadeBlocked: stored,
	}); err != nil {
		return fmt.Errorf("failed to set what blocks the cascade of task %q: %w", id, err)
	}

	return nil
}
