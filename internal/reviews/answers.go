package reviews

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/fail"
	reviewstore "komrad/internal/store/review"
)

// errReviewAddressedByOwner answers a reply with addressed from anyone but the writer of the task the thread is on.
var errReviewAddressedByOwner = fail.Denied("only the writer of the task a comment is on can reply with addressed")

// errReviewNothingAddressed answers an addressed reply that comes while the worktree is unchanged since the thread was written.
var errReviewNothingAddressed = fail.Precondition(
	"your worktree holds the same tree this comment was written on, so there is no change to show; " +
		"make the change first, or reply without addressed to push back or ask")

// errReviewNoAnswerSnapshot answers an addressed reply from an agent with no worktree to take a snapshot of.
var errReviewNoAnswerSnapshot = fail.Precondition("you have no worktree to take a snapshot of, so reply without addressed instead")

// replyHint is what a plain reply gets back when the worktree has moved since its thread was written.
const replyHint = "Your worktree has changed since this comment was written. " +
	"If that change answers it, reply again with addressed set so the reviewer can read " +
	"exactly what answered it."

// openAnswerSnapshot takes the snapshot an addressed reply from an agent points at, refusing one that changed nothing.
func (s *ReviewService) openAnswerSnapshot(ctx context.Context, thread reviewstore.Thread) (*reviewstore.Snapshot, error) {
	written, err := s.reviews.GetSnapshot(ctx, thread.SnapshotID)
	if err != nil {
		return nil, err
	}

	task, err := s.tasks.Get(ctx, thread.TaskID)
	if err != nil {
		return nil, err
	}

	answer, err := s.takeTaskSnapshot(ctx, *task)
	if errors.Is(err, ErrReviewNoSnapshot) || errors.Is(err, errReviewWorktreeRemoved) {
		return nil, errReviewNoAnswerSnapshot
	}
	if err != nil {
		return nil, fmt.Errorf("failed to take the snapshot that answers the comment: %w", err)
	}

	if answer.SHA == written.SHA {
		return nil, errReviewNothingAddressed
	}

	return answer, nil
}

// findReplyHint points a plain reply at addressed when the task's worktree no longer holds the
// tree its thread was written on, and says nothing when that cannot be read.
func (s *ReviewService) findReplyHint(ctx context.Context, thread reviewstore.Thread) string {
	written, err := s.reviews.GetSnapshot(ctx, thread.SnapshotID)
	if err != nil {
		return ""
	}

	task, err := s.tasks.Get(ctx, thread.TaskID)
	if err != nil || task.Checkout.Cwd == "" {
		return ""
	}

	now, err := s.freezeCheckout(ctx, *task)
	if err != nil || now.SHA == written.SHA {
		return ""
	}

	return replyHint
}
