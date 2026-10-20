package reviews

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/fail"
	"komrad/internal/reviews/history"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xslice"
)

const (
	midSequenceNotice     = "a rebase or merge is part way through, so this is the work as it stood before it"
	worktreeRemovedNotice = "the worktree was removed, so this is the work as it stood at its last snapshot"
)

type ReviewHistory struct {
	history.Record
	Snapshot  reviewstore.Snapshot
	Snapshots []reviewstore.Snapshot
	Threads   []ReviewThreadWithMessages
	Reviews   []reviewstore.Review
	Notice    string
}

type GetHistoryParams struct {
	Task   string
	Span   history.Span
	Author ReviewAuthor
}

// GetHistory reads a task's review as the panel opens it: the snapshot to read, the threads on it,
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

	snapshots, err := s.reviews.ListSnapshots(ctx, task.ID)
	if err != nil {
		return nil, err
	}

	picked, err := s.pickSnapshot(ctx, pickSnapshotParams{
		Task:      *task,
		Snapshots: snapshots,
	})
	if err != nil {
		return nil, err
	}

	dir, err := s.findSnapshotDir(ctx, *task)
	if err != nil {
		return nil, err
	}

	opened := &ReviewHistory{
		Snapshot:  picked.Snapshot,
		Snapshots: picked.Snapshots,
		Threads:   threads,
		Reviews:   reviews,
		Notice:    picked.Notice,
	}
	opened.Record = history.Build(ctx, history.BuildParams{
		Git:       s.git,
		Tracer:    s.tracer,
		Dir:       dir,
		Exclude:   s.readDiffExclude(ctx, task.WorkflowID),
		Snapshot:  historySnapshotOf(opened.Snapshot),
		Snapshots: xslice.Map(opened.Snapshots, historySnapshotOf),
		Reviews:   xslice.Map(reviews, historyReviewOf),
		Threads:   xslice.Map(threads, historyThreadOf),
		Span:      params.Span,
	})

	return opened, nil
}

type pickedSnapshot struct {
	Snapshot  reviewstore.Snapshot
	Snapshots []reviewstore.Snapshot
	Notice    string
}

type pickSnapshotParams struct {
	Task      taskstore.Task
	Snapshots []reviewstore.Snapshot
}

// pickSnapshot freezes the checkout as it stands, falling back to the newest snapshot while a rebase or merge is part way through.
func (s *ReviewService) pickSnapshot(ctx context.Context, params pickSnapshotParams) (*pickedSnapshot, error) {
	snapshots := params.Snapshots
	opened, err := s.takeTaskSnapshot(ctx, params.Task)
	var failed *fail.Error
	switch {
	case errors.As(err, &failed) && failed.Kind() == fail.KindPrecondition && len(snapshots) > 0:
		return &pickedSnapshot{
			Snapshot:  snapshots[len(snapshots)-1],
			Snapshots: snapshots,
			Notice:    explainFallback(err),
		}, nil
	case err != nil:
		return nil, err
	}

	if findStoreSnapshot(snapshots, opened.ID) == nil {
		snapshots = append(snapshots, *opened)
	}

	return &pickedSnapshot{Snapshot: *opened, Snapshots: snapshots}, nil
}

// explainFallback says why a review shows the newest snapshot rather than one taken now.
func explainFallback(err error) string {
	if errors.Is(err, errReviewWorktreeRemoved) {
		return worktreeRemovedNotice
	}

	return midSequenceNotice
}

// findSnapshotDir finds the directory holding a task's snapshots, its checkout or else its workflow's
// repository.
func (s *ReviewService) findSnapshotDir(ctx context.Context, task taskstore.Task) (string, error) {
	if !task.Checkout.WorktreeRemoved {
		return task.Checkout.Cwd, nil
	}

	flow, err := s.workflows.GetIncludingArchived(ctx, task.WorkflowID)
	if err != nil {
		return "", err
	}
	if flow.Cwd == "" {
		return "", fmt.Errorf("%w: %q", errReviewNoSnapshotDir, task.Slug)
	}

	return flow.Cwd, nil
}

func findStoreSnapshot(snapshots []reviewstore.Snapshot, id string) *reviewstore.Snapshot {
	for i := range snapshots {
		if snapshots[i].ID == id {
			return &snapshots[i]
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

func historySnapshotOf(snapshot reviewstore.Snapshot) history.Snapshot {
	return history.Snapshot{
		ID:      snapshot.ID,
		Ordinal: snapshot.Ordinal,
		BaseSHA: snapshot.BaseSHA,
		SHA:     snapshot.SHA,
	}
}

func historyReviewOf(review reviewstore.Review) history.Review {
	return history.Review{
		SnapshotID:      review.SnapshotID,
		SnapshotOrdinal: review.SnapshotOrdinal,
		Human:           review.AuthorKind == reviewstore.AuthorHuman,
		ReleasedAt:      review.ReleasedAt,
	}
}

func historyThreadOf(thread ReviewThreadWithMessages) history.Thread {
	return history.Thread{
		ID:         thread.Thread.ID,
		SnapshotID: thread.Thread.SnapshotID,
		FilePath:   thread.Thread.FilePath,
		NewStart:   thread.Thread.NewStart,
		NewEnd:     thread.Thread.NewEnd,
	}
}
