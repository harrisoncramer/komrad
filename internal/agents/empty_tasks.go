package agents

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/agenterr"
	"komrad/lib/xslice"

	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
)

// taskDeprovisioner gives back the checkout of a task no agent is left on.
type taskDeprovisioner interface {
	RunTaskDeprovision(ctx context.Context, task taskstore.Task, name string) error
}

type NewEmptyTaskDeleterParams struct {
	Tasks     *taskstore.TaskStore
	Workflows *workflowstore.WorkflowStore
	Scripts   taskDeprovisioner
}

// EmptyTaskDeleter removes a task being deleted once no agent is left on it, giving its checkout
// back first.
type EmptyTaskDeleter struct {
	tasks     *taskstore.TaskStore
	workflows *workflowstore.WorkflowStore
	scripts   taskDeprovisioner
}

// NewEmptyTaskDeleter builds the deleter over the stores and the deprovisioner a task's removal needs.
func NewEmptyTaskDeleter(params NewEmptyTaskDeleterParams) *EmptyTaskDeleter {
	return &EmptyTaskDeleter{
		tasks:     params.Tasks,
		workflows: params.Workflows,
		scripts:   params.Scripts,
	}
}

// DeleteIfEmpty permanently removes a task once it is being deleted and no agent is left on it,
// leaving it archived and no longer deleting, with the reason, when its checkout could not be
// given back.
func (d *EmptyTaskDeleter) DeleteIfEmpty(ctx context.Context, id string) error {
	claimed, err := d.tasks.ClaimEmpty(ctx, id)
	if err != nil || !claimed {
		return err
	}

	cleanup := context.WithoutCancel(ctx)
	found, err := d.tasks.Get(cleanup, id)
	if err != nil {
		return errors.Join(err, d.tasks.ClearDeleting(cleanup, id))
	}

	if err := d.deprovision(cleanup, *found); err != nil {
		failed := fmt.Errorf("%w: task %s: %w", agenterr.ErrTeardownFailed, found.Slug, err)

		return errors.Join(failed, d.tasks.RecordCleanupFailure(cleanup, found.ID, err))
	}

	if err := d.tasks.Delete(cleanup, found.ID); err != nil {
		return errors.Join(err, d.tasks.ClearDeleting(cleanup, found.ID))
	}

	return nil
}

// clearWorkflow claims every task in a workflow for a delete and removes each one no agent is
// left on, reporting whether the workflow holds no task afterwards.
func (d *EmptyTaskDeleter) clearWorkflow(ctx context.Context, workflowID string) (bool, error) {
	if err := d.tasks.MarkWorkflowDeleting(ctx, workflowID); err != nil {
		return false, err
	}

	ids, err := d.tasks.ListIDsInWorkflow(ctx, workflowID)
	if err != nil {
		return false, err
	}

	var failed []error
	for _, id := range ids {
		if err := d.DeleteIfEmpty(ctx, id); err != nil {
			failed = append(failed, err)
		}
	}
	if len(failed) > 0 {
		return false, errors.Join(failed...)
	}

	left, err := d.tasks.ListIDsInWorkflow(ctx, workflowID)
	if err != nil {
		return false, err
	}

	return len(left) == 0, nil
}

func (d *EmptyTaskDeleter) deprovision(ctx context.Context, task taskstore.Task) error {
	if d.scripts == nil || task.Checkout.Cwd == "" {
		return nil
	}

	flow, err := d.workflows.GetIncludingArchived(ctx, task.WorkflowID)
	if err != nil {
		return err
	}

	_, name := worktreeScripts(flow, nil)
	if name == "" {
		return nil
	}

	return d.scripts.RunTaskDeprovision(ctx, task, name)
}

func (d *EmptyTaskDeleter) markDeleting(ctx context.Context, ids []string) error {
	return d.tasks.MarkDeleting(ctx, ids)
}

func (d *EmptyTaskDeleter) release(ctx context.Context, ids []string) error {
	return errors.Join(xslice.Map(ids, func(id string) error {
		return d.tasks.ClearDeleting(ctx, id)
	})...)
}
