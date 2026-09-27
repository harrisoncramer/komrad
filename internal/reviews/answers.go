package reviews

import (
	"context"
	"fmt"

	"komrad/internal/fail"
	"komrad/internal/git"
	reviewstore "komrad/internal/store/review"
)

// ErrReviewAddressedByOwner is answered when anyone but the agent a thread is on replies with addressed.
var ErrReviewAddressedByOwner = fail.Denied("only the agent whose work a comment is on can reply with addressed")

// ErrReviewNothingAddressed is answered when an addressed reply carries the tree the thread was written on.
var ErrReviewNothingAddressed = fail.Precondition(
	"your worktree holds the same tree this comment was written on, so there is no change to show; " +
		"make the change first, or reply without addressed to push back or ask")

// replyHint is what a plain reply is told when the worktree has moved since its thread was written.
const replyHint = "Your worktree has changed since this comment was written. " +
	"If that change answers it, reply again with addressed set so the comment resolves " +
	"and the reviewer can read exactly what answered it."

// openAnswerRound takes the round an addressed reply points at, refusing one that changed nothing.
func (s *ReviewService) openAnswerRound(ctx context.Context, thread reviewstore.Thread, author ReviewAuthor) (*reviewstore.Round, error) {
	written, err := s.rounds.GetRound(ctx, thread.RoundID)
	if err != nil {
		return nil, err
	}

	answer, err := s.OpenRound(ctx, thread.AgentID, author)
	if err != nil {
		return nil, fmt.Errorf("failed to take the round that answers the comment: %w", err)
	}

	if answer.SnapshotSHA == written.SnapshotSHA {
		return nil, ErrReviewNothingAddressed
	}

	return answer, nil
}

// findReplyHint points a plain reply at addressed when the agent's worktree no longer holds the
// tree its thread was written on, and says nothing when that cannot be read.
func (s *ReviewService) findReplyHint(ctx context.Context, thread reviewstore.Thread) string {
	written, err := s.rounds.GetRound(ctx, thread.RoundID)
	if err != nil {
		return ""
	}

	agent, err := s.getLiveAgent(ctx, thread.AgentID)
	if err != nil || agent.Cwd == "" {
		return ""
	}

	base := ""
	if flow, err := s.workflows.Get(ctx, agent.WorkflowID); err == nil {
		base = flow.Worktree.BaseBranch
	}

	now, err := s.snapshots.TakeSnapshot(ctx, git.TakeSnapshotParams{Dir: agent.Cwd, Base: base})
	if err != nil || now.SHA == written.SnapshotSHA {
		return ""
	}

	return replyHint
}
