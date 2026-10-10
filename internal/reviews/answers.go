package reviews

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/fail"
	reviewstore "komrad/internal/store/review"
)

// errReviewAddressedByOwner is answered when anyone but the writer of the task a thread is on replies with addressed.
var errReviewAddressedByOwner = fail.Denied("only the writer of the task a comment is on can reply with addressed")

// errReviewNothingAddressed is answered when an addressed reply comes while the worktree is unchanged since the thread was written.
var errReviewNothingAddressed = fail.Precondition(
	"your worktree holds the same tree this comment was written on, so there is no change to show; " +
		"make the change first, or reply without addressed to push back or ask")

// errReviewNoAnswerRound is answered when an addressed reply comes from an agent with no worktree to take a round of.
var errReviewNoAnswerRound = fail.Precondition("you have no worktree to take a round of, so reply without addressed instead")

// replyHint is what a plain reply is told when the worktree has moved since its thread was written.
const replyHint = "Your worktree has changed since this comment was written. " +
	"If that change answers it, reply again with addressed set so the reviewer can read " +
	"exactly what answered it."

// openAnswerRound takes the round an addressed reply from an agent points at, refusing one that changed nothing.
func (s *ReviewService) openAnswerRound(ctx context.Context, thread reviewstore.Thread, agentID string) (*reviewstore.Round, error) {
	written, err := s.reviews.GetRound(ctx, thread.RoundID)
	if err != nil {
		return nil, err
	}

	task, err := s.tasks.Get(ctx, thread.TaskID)
	if err != nil {
		return nil, err
	}

	answer, err := s.openTaskRound(ctx, *task, agentID)
	if errors.Is(err, ErrReviewNoRound) || errors.Is(err, errReviewWorktreeRemoved) {
		return nil, errReviewNoAnswerRound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to take the round that answers the comment: %w", err)
	}

	if answer.SnapshotSHA == written.SnapshotSHA {
		return nil, errReviewNothingAddressed
	}

	return answer, nil
}

// findReplyHint points a plain reply at addressed when the task's worktree no longer holds the
// tree its thread was written on, and says nothing when that cannot be read.
func (s *ReviewService) findReplyHint(ctx context.Context, thread reviewstore.Thread) string {
	written, err := s.reviews.GetRound(ctx, thread.RoundID)
	if err != nil {
		return ""
	}

	task, err := s.tasks.Get(ctx, thread.TaskID)
	if err != nil || task.Checkout.Cwd == "" {
		return ""
	}

	now, err := s.takeSnapshot(ctx, *task)
	if err != nil || now.SHA == written.SnapshotSHA {
		return ""
	}

	return replyHint
}
