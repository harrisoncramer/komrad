package render

import (
	taskstore "komrad/internal/store/task"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	taskv1 "komrad/proto/gen/task/v1"
)

// Task draws one task as the wire carries it.
func Task(stored taskstore.Task) *taskv1.Task {
	checkout := stored.Checkout

	return &taskv1.Task{
		Id:                    stored.ID.String(),
		WorkflowId:            stored.WorkflowID.String(),
		Slug:                  stored.Slug,
		Title:                 stored.Title,
		Goal:                  stored.Goal,
		CreatedAt:             dates.Stamp(stored.CreatedAt),
		UpdatedAt:             dates.Stamp(stored.UpdatedAt),
		ArchivedAt:            dates.StampPtr(stored.ArchivedAt),
		Cwd:                   xptr.NonEmpty(checkout.Cwd),
		Branch:                checkout.Branch,
		RepositoryUrl:         checkout.RepositoryURL,
		HeadSha:               checkout.HeadSHA,
		WorktreeRemovalReason: checkout.WorktreeRemovalReason,
		Sequence:              checkout.Sequence,
		MovedSinceSnapshot:    checkout.MovedSinceSnapshot,
		DiffStat:              diffStatOf(checkout.DiffStat),
		AdoptedBranch:         xptr.NonEmpty(checkout.AdoptedBranch),
		FailureReason:         stored.FailureReason,
		Notes:                 stored.Notes,
		NotesUpdatedAt:        dates.StampPtr(stored.NotesUpdatedAt),
		FailedRun:             stored.FailedRun,
		Documents:             stored.Documents,
		DocumentsUpdatedAt:    dates.StampPtr(stored.DocumentsUpdatedAt),
		Snapshots:             stored.Snapshots,
		Hidden:                stored.Hidden,
		Parent:                parentOf(stored.Parent),
	}
}

func parentOf(parent *taskstore.Parent) *taskv1.Parent {
	if parent == nil {
		return nil
	}

	return &taskv1.Parent{
		TaskId:        parent.TaskID.String(),
		ForkPoint:     parent.ForkPoint,
		PullRequestId: xptr.Deref(formatOptionalID(parent.PullRequestID)),
		Blocked:       parent.Blocked,
	}
}

func diffStatOf(stat *taskstore.DiffStat) *taskv1.DiffStat {
	if stat == nil {
		return nil
	}

	return &taskv1.DiffStat{Added: int32(stat.Added), Removed: int32(stat.Removed)}
}
