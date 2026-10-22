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
		UpdatedAt:             dates.PreciseStamp(stored.UpdatedAt),
		ArchivedAt:            dates.StampPtr(stored.ArchivedAt),
		Cwd:                   xptr.NonEmpty(checkout.Cwd),
		Branch:                checkout.Branch,
		RepositoryUrl:         checkout.RepositoryURL,
		HeadSha:               checkout.HeadSHA,
		WorktreeRemovalReason: worktreeRemovalReasonOf(checkout.WorktreeRemovalReason),
		Sequence:              SequenceOf(checkout.Sequence),
		MovedSinceSnapshot:    checkout.MovedSinceSnapshot,
		DiffStat:              diffStatOf(checkout.DiffStat),
		AdoptedBranch:         xptr.NonEmpty(checkout.AdoptedBranch),
		FailureReason:         stored.FailureReason,
		Notes:                 stored.Notes,
		NotesUpdatedAt:        dates.PreciseStampPtr(stored.NotesUpdatedAt),
		FailedRun:             stored.FailedRun,
		Documents:             stored.Documents,
		DocumentsUpdatedAt:    dates.PreciseStampPtr(stored.DocumentsUpdatedAt),
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
		Blocked:       cascadeBlockOf(parent.Blocked),
	}
}

func cascadeBlockOf(block taskstore.CascadeBlock) taskv1.CascadeBlock {
	switch block {
	case taskstore.CascadeConflicted:
		return taskv1.CascadeBlock_CASCADE_BLOCK_CONFLICTED
	case taskstore.CascadeOrphaned:
		return taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED
	case taskstore.CascadeParentClosed:
		return taskv1.CascadeBlock_CASCADE_BLOCK_PARENT_CLOSED
	}

	return taskv1.CascadeBlock_CASCADE_BLOCK_UNSPECIFIED
}

func worktreeRemovalReasonOf(reason taskstore.WorktreeRemovalReason) taskv1.WorktreeRemovalReason {
	switch reason {
	case taskstore.WorktreeOnDisk:
		return taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED
	case taskstore.WorktreeDeleted:
		return taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED
	case taskstore.WorktreeGone:
		return taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
	}

	return taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED
}

// SequenceOf is a stored sequence as the wire carries it.
func SequenceOf(sequence taskstore.Sequence) taskv1.Sequence {
	switch sequence {
	case taskstore.SequenceNone:
		return taskv1.Sequence_SEQUENCE_NONE
	case taskstore.SequenceRebase:
		return taskv1.Sequence_SEQUENCE_REBASE
	case taskstore.SequenceApply:
		return taskv1.Sequence_SEQUENCE_AM
	case taskstore.SequenceMerge:
		return taskv1.Sequence_SEQUENCE_MERGE
	case taskstore.SequenceCherryPick:
		return taskv1.Sequence_SEQUENCE_CHERRY_PICK
	case taskstore.SequenceRevert:
		return taskv1.Sequence_SEQUENCE_REVERT
	}

	return taskv1.Sequence_SEQUENCE_UNSPECIFIED
}

func diffStatOf(stat *taskstore.DiffStat) *taskv1.DiffStat {
	if stat == nil {
		return nil
	}

	return &taskv1.DiffStat{Added: int32(stat.Added), Removed: int32(stat.Removed)}
}
