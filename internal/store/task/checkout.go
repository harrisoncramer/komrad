package task

import (
	"context"
	"encoding/json"
	"fmt"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/google/uuid"
)

// DiffStat is how many lines a checkout adds and removes.
type DiffStat struct {
	Added   int
	Removed int
}

// Checkout is the directory a task's agents work in and what the cwd syncer last read of it.
type Checkout struct {
	// Cwd is the checkout's directory, empty while the task has none.
	Cwd string
	// Branch is the branch checked out there, nil when git names none.
	Branch *string
	// AdoptedBranch is the branch the checkout was put on rather than one komrad made for it.
	AdoptedBranch string
	// WorktreeRemovalReason is why the worktree is no longer on disk, unspecified while it is.
	WorktreeRemovalReason taskv1.WorktreeRemovalReason
	// RepositoryURL is the canonical web address of the repository the checkout pushes to.
	RepositoryURL *string
	// HeadSHA is the commit the checkout has checked out.
	HeadSHA *string
	// Sequence is what git has part-way through the checkout and waiting on somebody.
	Sequence taskv1.Sequence
	// MovedSinceSnapshot is the newest snapshot's commit while the checkout no longer hashes to it.
	MovedSinceSnapshot string
	// DiffStat is the lines the checkout adds and removes since it left its base, nil where
	// there is no base to measure against.
	DiffStat *DiffStat
}

// IsUsable reports whether the checkout is a directory on disk its task's agents can work in.
func (c Checkout) IsUsable() bool {
	return c.Cwd != "" && !c.IsWorktreeRemoved()
}

// IsWorktreeRemoved reports whether the checkout's worktree is no longer on disk, for either reason.
func (c Checkout) IsWorktreeRemoved() bool {
	return c.WorktreeRemovalReason != taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED
}

func checkoutFrom(row *models.Task) (Checkout, error) {
	reason, err := fromDBToServiceNullWorktreeRemovalReason(row.WorktreeRemovalReason)
	if err != nil {
		return Checkout{}, err
	}
	sequence, err := fromDBToServiceNullSequence(row.Sequence)
	if err != nil {
		return Checkout{}, err
	}
	checkout := Checkout{
		Cwd:                   xptr.Deref(row.Cwd),
		Branch:                row.Branch,
		AdoptedBranch:         xptr.Deref(row.AdoptedBranch),
		RepositoryURL:         row.RepositoryUrl,
		HeadSHA:               row.HeadSha,
		Sequence:              sequence,
		WorktreeRemovalReason: reason,
		MovedSinceSnapshot:    xptr.Deref(row.MovedSinceSnapshot),
	}
	if row.LinesAdded != nil && row.LinesRemoved != nil {
		checkout.DiffStat = &DiffStat{Added: int(*row.LinesAdded), Removed: int(*row.LinesRemoved)}
	}

	return checkout, nil
}

// ClaimCheckoutParams is how a launch is to make a task's checkout.
type ClaimCheckoutParams struct {
	ID            uuid.UUID
	AdoptedBranch string
	RepositoryURL string
}

// ClaimCheckout records how a launch is to make a task's checkout, reporting false when the task already has one.
func (s *TaskStore) ClaimCheckout(ctx context.Context, params ClaimCheckoutParams) (bool, error) {
	claimed, err := s.queries.ClaimTaskCheckout(ctx, models.ClaimTaskCheckoutParams{
		ID:            params.ID,
		AdoptedBranch: xptr.NonEmpty(params.AdoptedBranch),
		RepositoryUrl: xptr.NonEmpty(params.RepositoryURL),
	})
	if err != nil {
		return false, fmt.Errorf("failed to claim the checkout of task %q: %w", params.ID, err)
	}

	return claimed > 0, nil
}

// ClaimCheckoutAgain claims a task's checkout again on the terms its last claim made, reporting false
// when the task already has one.
func (s *TaskStore) ClaimCheckoutAgain(ctx context.Context, id uuid.UUID) (bool, error) {
	claimed, err := s.queries.ClaimTaskCheckoutAgain(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to claim the checkout of task %q again: %w", id, err)
	}

	return claimed > 0, nil
}

// ReleaseCheckout gives up the claim on a task's checkout, leaving it free for another launch.
func (s *TaskStore) ReleaseCheckout(ctx context.Context, id uuid.UUID) error {
	if err := s.queries.ReleaseTaskCheckout(ctx, id); err != nil {
		return fmt.Errorf("failed to release the checkout of task %q: %w", id, err)
	}

	return nil
}

// SetCheckoutDir records the directory a launch provisioned for a task.
func (s *TaskStore) SetCheckoutDir(ctx context.Context, id uuid.UUID, cwd string) error {
	if err := s.queries.SetTaskCheckoutDir(ctx, models.SetTaskCheckoutDirParams{
		ID:  id,
		Cwd: xptr.NonEmpty(cwd),
	}); err != nil {
		return fmt.Errorf("failed to set the checkout of task %q: %w", id, err)
	}

	return nil
}

// SetHeadParams is what the cwd syncer read of a task's checkout.
type SetHeadParams struct {
	// Branch is the branch checked out there, nil when git names none.
	Branch *string
	// RepositoryURL is the repository the checkout pushes to, nil when it names none.
	RepositoryURL *string
	// HeadSHA is the commit checked out there, nil when git reads none.
	HeadSHA *string
	// WorktreeRemovalReason is gone when the checkout's directory is gone, unspecified while it is there.
	WorktreeRemovalReason taskv1.WorktreeRemovalReason
	Sequence              taskv1.Sequence
}

// SetHead records what the cwd syncer read of a task's checkout.
func (s *TaskStore) SetHead(ctx context.Context, id uuid.UUID, params SetHeadParams) error {
	reason, err := fromServiceToDBNullWorktreeRemovalReason(params.WorktreeRemovalReason)
	if err != nil {
		return err
	}
	sequence, err := fromServiceToDBNullSequence(params.Sequence)
	if err != nil {
		return err
	}

	if err := s.queries.SetTaskHead(ctx, models.SetTaskHeadParams{
		ID:                    id,
		Branch:                params.Branch,
		RepositoryUrl:         params.RepositoryURL,
		HeadSha:               params.HeadSHA,
		WorktreeRemoved:       xptr.Ptr(params.WorktreeRemovalReason != taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED),
		WorktreeRemovalReason: reason,
		Sequence:              sequence,
	}); err != nil {
		return fmt.Errorf("failed to set the head of task %q: %w", id, err)
	}

	return nil
}

// SetSequence records what git has part-way through a task's checkout.
func (s *TaskStore) SetSequence(ctx context.Context, id uuid.UUID, sequence taskv1.Sequence) error {
	stored, err := fromServiceToDBNullSequence(sequence)
	if err != nil {
		return err
	}

	if err := s.queries.SetTaskSequence(ctx, models.SetTaskSequenceParams{
		ID:       id,
		Sequence: stored,
	}); err != nil {
		return fmt.Errorf("failed to set the sequence of task %q: %w", id, err)
	}

	return nil
}

// SetMovedSinceSnapshot records the newest snapshot's commit a task's checkout has moved away from,
// or clears it with an empty one.
func (s *TaskStore) SetMovedSinceSnapshot(ctx context.Context, id uuid.UUID, snapshotSHA string) error {
	if err := s.queries.SetTaskMovedSinceSnapshot(ctx, models.SetTaskMovedSinceSnapshotParams{
		ID:                 id,
		MovedSinceSnapshot: &snapshotSHA,
	}); err != nil {
		return fmt.Errorf("failed to set the snapshot task %q moved since: %w", id, err)
	}

	return nil
}

// SetDiffStat records the lines a task's checkout adds and removes, or clears them with nil.
func (s *TaskStore) SetDiffStat(ctx context.Context, id uuid.UUID, stat *DiffStat) error {
	params := models.SetTaskDiffStatParams{ID: id}
	if stat != nil {
		params.LinesAdded, params.LinesRemoved = xptr.Ptr(int32(stat.Added)), xptr.Ptr(int32(stat.Removed))
	}

	if err := s.queries.SetTaskDiffStat(ctx, params); err != nil {
		return fmt.Errorf("failed to set the diff stat of task %q: %w", id, err)
	}

	return nil
}

// SetLastRebase records what the latest rebase of a task's checkout came to.
func (s *TaskStore) SetLastRebase(ctx context.Context, id uuid.UUID, rebase json.RawMessage) error {
	if err := s.queries.SetTaskLastRebase(ctx, models.SetTaskLastRebaseParams{ID: id, LastRebase: rebase}); err != nil {
		return fmt.Errorf("failed to set the last rebase of task %q: %w", id, err)
	}

	return nil
}

// GetLastRebase reads what the latest rebase of a task's checkout came to, nil when it was never rebased.
func (s *TaskStore) GetLastRebase(ctx context.Context, id uuid.UUID) (json.RawMessage, error) {
	rebase, err := s.queries.GetTaskLastRebase(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the last rebase of task %q: %w", id, err)
	}

	return rebase, nil
}

// ListCheckouts returns every live task with a checkout directory and an agent still on it, leaving
// out a task whose worktree was deleted.
func (s *TaskStore) ListCheckouts(ctx context.Context) ([]Task, error) {
	rows, err := s.queries.ListTaskCheckouts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the task checkouts: %w", err)
	}

	return fromRows(rows)
}

// ListByID returns the tasks among ids, archived or not.
func (s *TaskStore) ListByID(ctx context.Context, ids []uuid.UUID) ([]Task, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListTasksByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list %d tasks: %w", len(ids), err)
	}

	return xslice.TryMap(rows, func(row *models.ListTasksByIDRow) (Task, error) {
		task, err := fromCountedRow(&row.Task, row.Snapshots)
		if err != nil {
			return Task{}, err
		}

		return *task, nil
	})
}
