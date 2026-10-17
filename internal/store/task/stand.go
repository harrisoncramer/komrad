package task

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

// Stand is the task another task's branch was cut from, and where on it the task's own commits begin.
type Stand struct {
	// ParentID is the task stood on.
	ParentID string
	// Tip is the commit of the parent's branch the task's own commits sit on.
	Tip string
	// PullRequestID is the parent's pull request the task follows, empty until the parent has one.
	PullRequestID string
	// Blocked is why komrad cannot restack the task, empty while nothing does.
	Blocked restackBlock
}

func standFrom(row *models.Task) *Stand {
	if row.StandsOnTaskID == nil {
		return nil
	}

	return &Stand{
		ParentID:      *row.StandsOnTaskID,
		Tip:           xptr.Deref(row.StandTip),
		PullRequestID: xptr.Deref(row.StandPullRequestID),
		Blocked:       restackBlock(xptr.Deref(row.RestackBlocked)),
	}
}

// SetStand rewrites what a task stands on, or clears it with nil.
func (s *TaskStore) SetStand(ctx context.Context, id string, stand *Stand) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	params := models.SetTaskStandParams{ID: id}
	if stand != nil {
		params.StandsOnTaskID = &stand.ParentID
		params.StandTip = xptr.NonEmpty(stand.Tip)
		params.StandPullRequestID = xptr.NonEmpty(stand.PullRequestID)
	}

	if err := s.queries.SetTaskStand(ctx, params); err != nil {
		return fmt.Errorf("failed to set what task %q stands on: %w", id, err)
	}

	return nil
}

// SetStandTip records the commit of the parent's branch a standing task's own commits now sit on.
func (s *TaskStore) SetStandTip(ctx context.Context, id, tip string) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetTaskStandTip(ctx, models.SetTaskStandTipParams{ID: id, StandTip: &tip}); err != nil {
		return fmt.Errorf("failed to set the tip task %q stands on: %w", id, err)
	}

	return nil
}

// RecordStandPullRequest records the parent's pull request a standing task follows, reporting
// false when it already follows one.
func (s *TaskStore) RecordStandPullRequest(ctx context.Context, id, pullRequestID string) (bool, error) {
	if !store.IsID(id) || !store.IsID(pullRequestID) {
		return false, fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	recorded, err := s.queries.RecordTaskStandPullRequest(ctx, models.RecordTaskStandPullRequestParams{
		ID:                 id,
		StandPullRequestID: &pullRequestID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to record the pull request task %q follows: %w", id, err)
	}

	return recorded > 0, nil
}

// ListStanding returns every live task that stands on another.
func (s *TaskStore) ListStanding(ctx context.Context) ([]Task, error) {
	rows, err := s.queries.ListStandingTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the standing tasks: %w", err)
	}

	return xslice.Map(rows, fromRow), nil
}

// ListStandingOn returns the undeleted tasks standing on any of ids, archived or not.
func (s *TaskStore) ListStandingOn(ctx context.Context, ids []string) ([]Task, error) {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListTasksStandingOn(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the tasks standing on %d tasks: %w", len(storable), err)
	}

	return xslice.Map(rows, fromRow), nil
}

// BranchName is the branch a task's checkout is on, or the one komrad cuts for it while it has none.
func (t Task) BranchName() string {
	if branch := xptr.Deref(t.Checkout.Branch); branch != "" {
		return branch
	}
	if t.Checkout.AdoptedBranch != "" {
		return t.Checkout.AdoptedBranch
	}

	return "task/" + t.ID
}

// restackBlock is why komrad cannot restack a standing task onto its parent.
type restackBlock string

const (
	// RestackConflicted is a replay onto the parent that conflicted.
	RestackConflicted restackBlock = "conflicts"
	// RestackLost is a branch holding neither the recorded tip nor the parent's.
	RestackLost restackBlock = "lost"
	// RestackParentClosed is a parent whose pull request closed without merging.
	RestackParentClosed restackBlock = "closed"
)

// SetRestackBlocked records why komrad cannot restack a task, or clears it with an empty block.
func (s *TaskStore) SetRestackBlocked(ctx context.Context, id string, block restackBlock) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetTaskRestackBlocked(ctx, models.SetTaskRestackBlockedParams{
		ID:             id,
		RestackBlocked: xptr.NonEmpty(string(block)),
	}); err != nil {
		return fmt.Errorf("failed to set what blocks the restack of task %q: %w", id, err)
	}

	return nil
}
