package tasks

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/agenterr"
	"komrad/internal/scriptrun"
	"komrad/lib/xslice"

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

	if err := s.worktrees.DeleteForTask(cleanup, found.ID); err != nil {
		return errors.Join(err, s.tasks.ClearDeleting(cleanup, found.ID))
	}

	if err := s.tasks.Delete(cleanup, found.ID); err != nil {
		return errors.Join(err, s.tasks.ClearDeleting(cleanup, found.ID))
	}

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
