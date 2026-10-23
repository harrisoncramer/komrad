package task

import (
	"fmt"

	"komrad/db/models"
	taskv1 "komrad/proto/gen/task/v1"
)

// fromServiceToDBSequence is the stored value of a sequence.
func fromServiceToDBSequence(sequence taskv1.Sequence) (models.AgentSequence, error) {
	switch sequence {
	case taskv1.Sequence_SEQUENCE_NONE:
		return models.AgentSequenceNone, nil
	case taskv1.Sequence_SEQUENCE_REBASE:
		return models.AgentSequenceRebase, nil
	case taskv1.Sequence_SEQUENCE_AM:
		return models.AgentSequenceAm, nil
	case taskv1.Sequence_SEQUENCE_MERGE:
		return models.AgentSequenceMerge, nil
	case taskv1.Sequence_SEQUENCE_CHERRY_PICK:
		return models.AgentSequenceCherryPick, nil
	case taskv1.Sequence_SEQUENCE_REVERT:
		return models.AgentSequenceRevert, nil
	case taskv1.Sequence_SEQUENCE_UNSPECIFIED:
	}

	return "", fmt.Errorf("a sequence %v has no stored value", sequence)
}

// fromDBToServiceSequence is the sequence a stored value names.
func fromDBToServiceSequence(sequence models.AgentSequence) (taskv1.Sequence, error) {
	switch sequence {
	case models.AgentSequenceNone:
		return taskv1.Sequence_SEQUENCE_NONE, nil
	case models.AgentSequenceRebase:
		return taskv1.Sequence_SEQUENCE_REBASE, nil
	case models.AgentSequenceAm:
		return taskv1.Sequence_SEQUENCE_AM, nil
	case models.AgentSequenceMerge:
		return taskv1.Sequence_SEQUENCE_MERGE, nil
	case models.AgentSequenceCherryPick:
		return taskv1.Sequence_SEQUENCE_CHERRY_PICK, nil
	case models.AgentSequenceRevert:
		return taskv1.Sequence_SEQUENCE_REVERT, nil
	}

	return taskv1.Sequence_SEQUENCE_UNSPECIFIED, fmt.Errorf("stored sequence %q has no service value", sequence)
}

// fromServiceToDBNullSequence is the stored value of a sequence, none for an unspecified one.
func fromServiceToDBNullSequence(sequence taskv1.Sequence) (models.NullAgentSequence, error) {
	if sequence == taskv1.Sequence_SEQUENCE_UNSPECIFIED {
		sequence = taskv1.Sequence_SEQUENCE_NONE
	}

	stored, err := fromServiceToDBSequence(sequence)
	if err != nil {
		return models.NullAgentSequence{}, err
	}

	return models.NullAgentSequence{AgentSequence: stored, Valid: true}, nil
}

// fromDBToServiceNullSequence is the sequence a stored value names, none for a null one.
func fromDBToServiceNullSequence(sequence models.NullAgentSequence) (taskv1.Sequence, error) {
	if !sequence.Valid {
		return taskv1.Sequence_SEQUENCE_NONE, nil
	}

	return fromDBToServiceSequence(sequence.AgentSequence)
}

// fromServiceToDBNullWorktreeRemovalReason is the stored value of a worktree removal reason, null for an unspecified one.
func fromServiceToDBNullWorktreeRemovalReason(reason taskv1.WorktreeRemovalReason) (models.NullWorktreeRemovalReason, error) {
	switch reason {
	case taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED:
		return models.NullWorktreeRemovalReason{}, nil
	case taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED:
		return models.NullWorktreeRemovalReason{WorktreeRemovalReason: models.WorktreeRemovalReasonDeleted, Valid: true}, nil
	case taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE:
		return models.NullWorktreeRemovalReason{WorktreeRemovalReason: models.WorktreeRemovalReasonGone, Valid: true}, nil
	}

	return models.NullWorktreeRemovalReason{}, fmt.Errorf("a worktree removal reason %v has no stored value", reason)
}

// fromDBToServiceNullWorktreeRemovalReason is the worktree removal reason a stored value names, unspecified for a null one.
func fromDBToServiceNullWorktreeRemovalReason(reason models.NullWorktreeRemovalReason) (taskv1.WorktreeRemovalReason, error) {
	if !reason.Valid {
		return taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED, nil
	}

	switch reason.WorktreeRemovalReason {
	case models.WorktreeRemovalReasonDeleted:
		return taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED, nil
	case models.WorktreeRemovalReasonGone:
		return taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE, nil
	}

	return taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED, fmt.Errorf("stored worktree removal reason %q has no service value", reason.WorktreeRemovalReason)
}

// fromServiceToDBCascadeBlock is the stored text of a cascade block.
func fromServiceToDBCascadeBlock(block taskv1.CascadeBlock) (string, error) {
	switch block {
	case taskv1.CascadeBlock_CASCADE_BLOCK_CONFLICTED:
		return "conflicts", nil
	case taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED:
		return "orphaned", nil
	case taskv1.CascadeBlock_CASCADE_BLOCK_PARENT_CLOSED:
		return "closed", nil
	case taskv1.CascadeBlock_CASCADE_BLOCK_UNSPECIFIED:
	}

	return "", fmt.Errorf("a cascade block %v has no stored value", block)
}

// fromDBToServiceCascadeBlock is the cascade block stored text names, unspecified for a null one.
func fromDBToServiceCascadeBlock(stored *string) (taskv1.CascadeBlock, error) {
	if stored == nil {
		return taskv1.CascadeBlock_CASCADE_BLOCK_UNSPECIFIED, nil
	}

	switch *stored {
	case "conflicts":
		return taskv1.CascadeBlock_CASCADE_BLOCK_CONFLICTED, nil
	case "orphaned":
		return taskv1.CascadeBlock_CASCADE_BLOCK_ORPHANED, nil
	case "closed":
		return taskv1.CascadeBlock_CASCADE_BLOCK_PARENT_CLOSED, nil
	}

	return taskv1.CascadeBlock_CASCADE_BLOCK_UNSPECIFIED, fmt.Errorf("stored cascade block %q has no service value", *stored)
}
