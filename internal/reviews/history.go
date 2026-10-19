package reviews

import (
	"context"
	"errors"

	"komrad/internal/fail"
	"komrad/internal/reviews/history"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xslice"
)

const midSequenceNotice = "a rebase or merge is part way through, so this is the work as it stood before it"

type ReviewHistory struct {
	history.Record
	Round   reviewstore.Round
	Rounds  []reviewstore.Round
	Threads []ReviewThreadWithMessages
	Reviews []reviewstore.Review
	Notice  string
}

type GetHistoryParams struct {
	Task   string
	Span   history.Span
	Author ReviewAuthor
}

// GetHistory reads a task's review as the panel opens it: the round to read, the threads on it,
// and the history built from them.
func (s *ReviewService) GetHistory(ctx context.Context, params GetHistoryParams) (*ReviewHistory, error) {
	task, err := s.getTask(ctx, params.Task, params.Author)
	if err != nil {
		return nil, err
	}
	if err := s.requireMayRead(ctx, *task, params.Author); err != nil {
		return nil, err
	}

	threads, err := s.listThreads(ctx, task.ID, params.Author)
	if err != nil {
		return nil, err
	}

	reviews, err := s.listReviews(ctx, task.ID, params.Author)
	if err != nil {
		return nil, err
	}

	rounds, err := s.reviews.ListRounds(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	picked, err := s.pickRound(ctx, pickRoundParams{
		Task:   *task,
		Rounds: rounds,
	})
	if err != nil {
		return nil, err
	}

	opened := &ReviewHistory{
		Round:   picked.Round,
		Rounds:  picked.Rounds,
		Threads: threads,
		Reviews: reviews,
		Notice:  picked.Notice,
	}
	opened.Record = history.Build(ctx, history.BuildParams{
		Git:     s.git,
		Tracer:  s.tracer,
		Dir:     task.Checkout.Cwd,
		Exclude: s.readDiffExclude(ctx, task.WorkflowID),
		Round:   historyRoundOf(opened.Round),
		Rounds:  xslice.Map(opened.Rounds, historyRoundOf),
		Reviews: xslice.Map(reviews, historyReviewOf),
		Threads: xslice.Map(threads, historyThreadOf),
		Span:    params.Span,
	})

	return opened, nil
}

type pickedRound struct {
	Round  reviewstore.Round
	Rounds []reviewstore.Round
	Notice string
}

type pickRoundParams struct {
	Task   taskstore.Task
	Rounds []reviewstore.Round
}

// pickRound freezes the checkout as it stands, falling back to the newest round while a rebase or merge is part way through.
func (s *ReviewService) pickRound(ctx context.Context, params pickRoundParams) (*pickedRound, error) {
	rounds := params.Rounds
	opened, err := s.openTaskRound(ctx, params.Task)
	var failed *fail.Error
	switch {
	case errors.As(err, &failed) && failed.Kind() == fail.KindPrecondition && len(rounds) > 0:
		return &pickedRound{
			Round:  rounds[len(rounds)-1],
			Rounds: rounds,
			Notice: midSequenceNotice,
		}, nil
	case err != nil:
		return nil, err
	}

	if findStoreRound(rounds, opened.ID) == nil {
		rounds = append(rounds, *opened)
	}

	return &pickedRound{Round: *opened, Rounds: rounds}, nil
}

func findStoreRound(rounds []reviewstore.Round, id string) *reviewstore.Round {
	for i := range rounds {
		if rounds[i].ID == id {
			return &rounds[i]
		}
	}

	return nil
}

func (s *ReviewService) readDiffExclude(ctx context.Context, workflowID string) []string {
	flow, err := s.workflows.Get(ctx, workflowID)
	if err != nil {
		return nil
	}

	return flow.Worktree.DiffExclude
}

func historyRoundOf(round reviewstore.Round) history.Round {
	return history.Round{
		ID:          round.ID,
		Ordinal:     round.Ordinal,
		BaseSHA:     round.BaseSHA,
		SnapshotSHA: round.SnapshotSHA,
	}
}

func historyReviewOf(review reviewstore.Review) history.Review {
	return history.Review{
		RoundID:      review.RoundID,
		RoundOrdinal: review.RoundOrdinal,
		Human:        review.AuthorKind == reviewstore.AuthorHuman,
		ReleasedAt:   review.ReleasedAt,
	}
}

func historyThreadOf(thread ReviewThreadWithMessages) history.Thread {
	return history.Thread{
		ID:       thread.Thread.ID,
		RoundID:  thread.Thread.RoundID,
		FilePath: thread.Thread.FilePath,
		NewStart: thread.Thread.NewStart,
		NewEnd:   thread.Thread.NewEnd,
	}
}
