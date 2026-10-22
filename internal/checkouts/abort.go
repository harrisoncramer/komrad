package checkouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/logging"
	taskstore "komrad/internal/store/task"
	"komrad/proto/gen/oneofs"
)

// ErrAbortNothing says nothing is part-way through the checkout.
var ErrAbortNothing = fail.Precondition("nothing is part-way through in this worktree, so there is nothing to abort")

// AbortResult is what an abort did.
type AbortResult struct {
	Sequence taskstore.Sequence `json:"sequence"`
	// Command is the git command that ran the sequence.
	Command string `json:"command"`
	// Branch is where the checkout stands once the sequence is undone, empty when detached.
	Branch string `json:"branch"`
	// ParkedRef holds the checkout as it stood mid-sequence, conflicts and all.
	ParkedRef string `json:"parked_ref"`
	// Ours says the sequence was komrad's own rebase, stopped on conflicts.
	Ours bool `json:"ours"`
}

// Abort undoes whatever rebase, am, merge or pick is part-way through the task's worktree,
// parking the checkout first so nothing its writer had resolved is lost.
func (s *Service) Abort(ctx context.Context, task oneofs.Task) (*AbortResult, error) {
	target, err := s.eligible(ctx, task)
	if err != nil {
		return nil, err
	}

	unlock, err := s.lockTree(target.dir())
	if err != nil {
		return nil, err
	}
	defer unlock()

	if _, err := s.readSequence(ctx, target.dir()); err != nil {
		return nil, err
	}

	result, err := s.abort(ctx, *target)
	if err != nil {
		return nil, err
	}

	s.queueEvent(ctx, target.task.ID, aborted{TaskID: target.task.ID, Result: result})

	return result, nil
}

// abort reads the sequence again, parks the checkout and undoes it.
func (s *Service) abort(ctx context.Context, target checkoutTarget) (*AbortResult, error) {
	if err := s.recheckWriter(ctx, target); err != nil {
		return nil, err
	}

	sequence, err := s.readSequence(ctx, target.dir())
	if err != nil {
		return nil, err
	}

	ours, err := s.isOwnRebase(ctx, target, sequence)
	if err != nil {
		return nil, err
	}

	result := &AbortResult{
		Sequence:  sequenceOf(sequence),
		Command:   sequence.GetCommand(),
		ParkedRef: buildParkRef(parkAbort, target.task.ID),
		Ours:      ours,
	}
	if _, err := s.git.Park(ctx, target.dir(), result.ParkedRef); err != nil {
		return nil, err
	}
	defer s.checkoutSync.Wake()

	err = s.git.AbortSequence(ctx, target.dir(), sequence)
	if errors.Is(err, git.ErrIndexLocked) {
		return nil, fmt.Errorf("%w: %w", errBusy, err)
	}
	if err != nil {
		return nil, err
	}

	s.recordAborted(ctx, *target.task)

	state, err := s.git.ReadTreeState(ctx, target.dir())
	if err != nil {
		return nil, err
	}
	result.Branch = state.Branch

	return result, nil
}

// readSequence is what is part-way through the checkout, refusing one with nothing.
func (s *Service) readSequence(ctx context.Context, dir string) (git.Sequence, error) {
	sequence, err := s.git.ReadSequence(ctx, dir)
	if err != nil {
		return git.SequenceNone, err
	}
	if !sequence.IsInProgress() {
		return git.SequenceNone, ErrAbortNothing
	}

	return sequence, nil
}

// isOwnRebase reports whether the sequence is the rebase komrad started and left stopped on
// conflicts, which is the one the writer was told to wait on.
func (s *Service) isOwnRebase(ctx context.Context, target checkoutTarget, sequence git.Sequence) (bool, error) {
	if sequence != git.SequenceRebase {
		return false, nil
	}

	raw, err := s.tasks.GetLastRebase(ctx, target.task.ID)
	if err != nil {
		return false, err
	}
	if raw == nil {
		return false, nil
	}

	var outcome rebaseOutcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return false, fmt.Errorf("failed to read the last rebase of task %q: %w", target.task.Slug, err)
	}
	if !outcome.Result.Conflicted {
		return false, nil
	}

	onto, err := s.git.ReadRebaseOnto(ctx, target.dir())
	if err != nil {
		return false, err
	}

	return onto != "" && onto == outcome.Result.BaseSHA, nil
}

// recordAborted clears the sequence on the task ahead of the checkout sync.
func (s *Service) recordAborted(ctx context.Context, task taskstore.Task) {
	if err := s.tasks.SetSequence(ctx, task.ID, taskstore.SequenceNone); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to record that a task's sequence was aborted",
			append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)
	}
}
