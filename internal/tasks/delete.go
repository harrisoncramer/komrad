package tasks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"komrad/internal/agenterr"
	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	"komrad/lib/xslice"

	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
)

// MarkDeleting claims tasks for a delete.
func (s *TaskService) MarkDeleting(ctx context.Context, ids []string) error {
	return s.tasks.MarkDeleting(ctx, ids)
}

// ClearDeleting releases the delete claim on tasks.
func (s *TaskService) ClearDeleting(ctx context.Context, ids []string) error {
	return errors.Join(xslice.Map(ids, func(id string) error {
		return s.tasks.ClearDeleting(ctx, id)
	})...)
}

// DeleteIfEmpty permanently removes a task once it is being deleted and no agent is left on it,
// leaving it archived and no longer deleting, with the reason, when its checkout could not be
// given back.
func (s *TaskService) DeleteIfEmpty(ctx context.Context, id string) error {
	claimed, err := s.tasks.ClaimEmpty(ctx, id)
	if err != nil || !claimed {
		return err
	}

	cleanup := context.WithoutCancel(ctx)
	found, err := s.tasks.Get(cleanup, id)
	if err != nil {
		return errors.Join(err, s.tasks.ClearDeleting(cleanup, id))
	}

	if err := s.tearDown(cleanup, *found); err != nil {
		failed := fmt.Errorf("%w: task %s: %w", agenterr.ErrTeardownFailed, found.Slug, err)

		return errors.Join(failed, s.tasks.RecordCleanupFailure(cleanup, found.ID, err))
	}

	held := s.findRoundRefs(cleanup, *found)

	if err := s.worktrees.DeleteForTask(cleanup, found.ID); err != nil {
		return errors.Join(err, s.tasks.ClearDeleting(cleanup, found.ID))
	}

	if err := s.tasks.Delete(cleanup, found.ID); err != nil {
		return errors.Join(err, s.tasks.ClearDeleting(cleanup, found.ID))
	}

	s.releaseRoundRefs(cleanup, held)

	return nil
}

// ClearWorkflow claims every task in a workflow for a delete and removes each one no agent is
// left on, reporting whether the workflow holds no task afterwards.
func (s *TaskService) ClearWorkflow(ctx context.Context, workflowID string) (bool, error) {
	if err := s.tasks.MarkWorkflowDeleting(ctx, workflowID); err != nil {
		return false, err
	}

	ids, err := s.tasks.ListIDsInWorkflow(ctx, workflowID)
	if err != nil {
		return false, err
	}

	var failed []error
	for _, id := range ids {
		if err := s.DeleteIfEmpty(ctx, id); err != nil {
			failed = append(failed, err)
		}
	}
	if len(failed) > 0 {
		return false, errors.Join(failed...)
	}

	left, err := s.tasks.ListIDsInWorkflow(ctx, workflowID)
	if err != nil {
		return false, err
	}

	return len(left) == 0, nil
}

// tearDown runs a task's pre-delete hooks once any of its hooks still running have finished, then gives back its checkout.
func (s *TaskService) tearDown(ctx context.Context, task taskstore.Task) error {
	if s.scripts == nil {
		return nil
	}

	flow, err := s.workflows.GetIncludingArchived(ctx, task.WorkflowID)
	if err != nil {
		return err
	}

	if err := s.scripts.AwaitHooks(ctx, task.ID); err != nil {
		return err
	}

	if err := (moment{
		scripts:  s.scripts,
		workflow: flow,
		task:     task,
		event:    workflowstore.HookTaskPreDelete,
	}.toHookRun().RunInTurn(ctx)); err != nil {
		return err
	}

	return s.deprovision(ctx, task, flow)
}

func (s *TaskService) deprovision(ctx context.Context, task taskstore.Task, flow *workflowstore.Workflow) error {
	if task.Checkout.Cwd == "" {
		return nil
	}

	_, deprovisioner := scriptrun.WorktreeScripts(flow, nil)
	if deprovisioner == "" {
		return nil
	}

	return s.scripts.RunTaskDeprovision(ctx, task, deprovisioner)
}

// roundRefs is the refs holding the snapshots of a task's rounds, and a checkout they are held in.
type roundRefs struct {
	taskID string
	dir    string
	refs   []string
}

// findRoundRefs reads the refs of a task's rounds while its rows still stand, so they can be let
// go of once the task is gone.
func (s *TaskService) findRoundRefs(ctx context.Context, task taskstore.Task) roundRefs {
	if s.reviews == nil {
		return roundRefs{}
	}

	rounds, err := s.reviews.ListRounds(ctx, task.ID)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to read the rounds of a task being deleted",
			append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)

		return roundRefs{}
	}
	if len(rounds) == 0 {
		return roundRefs{}
	}

	dir, err := s.findRoundRefDir(ctx, task)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to find a checkout holding the rounds of a task being deleted",
			append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)

		return roundRefs{}
	}

	return roundRefs{
		taskID: task.ID,
		dir:    dir,
		refs: xslice.Map(rounds, func(round reviewstore.Round) string {
			return reviewstore.RoundRef(task.ID, round.SnapshotSHA)
		}),
	}
}

// releaseRoundRefs drops the refs holding a deleted task's snapshots, which would otherwise pin a
// commit covering the whole worktree long after the task is gone.
func (s *TaskService) releaseRoundRefs(ctx context.Context, held roundRefs) {
	for _, ref := range held.refs {
		if err := s.snapshots.ReleaseRef(ctx, held.dir, ref); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the snapshot of a round",
				append([]slog.Attr{slog.String(logging.TaskID, held.taskID)}, logging.Err(err)...)...)
		}
	}
}

// findRoundRefDir is a checkout of the repository holding a task's round refs: the task's own
// while it stands, else its workflow's directory, which shares the refs of every worktree cut from
// it.
func (s *TaskService) findRoundRefDir(ctx context.Context, task taskstore.Task) (string, error) {
	if info, err := os.Stat(task.Checkout.Cwd); err == nil && info.IsDir() {
		return task.Checkout.Cwd, nil
	}

	flow, err := s.workflows.GetIncludingArchived(ctx, task.WorkflowID)
	if err != nil {
		return "", err
	}

	return flow.Cwd, nil
}
