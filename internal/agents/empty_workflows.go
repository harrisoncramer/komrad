package agents

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/store"
	"komrad/internal/tasks"

	agentstore "komrad/internal/store/agent"
	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"

	"github.com/google/uuid"
)

type NewEmptyWorkflowDeleterParams struct {
	Workflows *workflowstore.WorkflowStore
	Agents    *agentstore.AgentStore
	Schedules *schedulestore.ScheduleStore
	Tasks     *tasks.TaskService
}

// EmptyWorkflowDeleter removes an archived workflow once nothing is left inside it, taking its
// tasks and the archived schedules pointing at it with the row and deleting no agents itself.
type EmptyWorkflowDeleter struct {
	workflows *workflowstore.WorkflowStore
	agents    *agentstore.AgentStore
	schedules *schedulestore.ScheduleStore
	tasks     *tasks.TaskService
}

// NewEmptyWorkflowDeleter builds the deleter over the stores a workflow's removal touches.
func NewEmptyWorkflowDeleter(params NewEmptyWorkflowDeleterParams) *EmptyWorkflowDeleter {
	return &EmptyWorkflowDeleter{
		workflows: params.Workflows,
		agents:    params.Agents,
		schedules: params.Schedules,
		tasks:     params.Tasks,
	}
}

// DeleteIfEmpty permanently removes a workflow once it is archived and holds no agents at all.
func (d *EmptyWorkflowDeleter) DeleteIfEmpty(ctx context.Context, id uuid.UUID) error {
	existing, err := d.workflows.GetIncludingArchived(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if existing.ArchivedAt == nil {
		return nil
	}

	return d.deleteIfEmpty(ctx, existing)
}

func (d *EmptyWorkflowDeleter) deleteIfEmpty(ctx context.Context, existing *workflowstore.Workflow) error {
	remaining, err := d.agents.ListArchivedInWorkflow(ctx, existing.ID)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return nil
	}

	return d.deleteWithSchedules(ctx, existing)
}

func (d *EmptyWorkflowDeleter) deleteWithSchedules(ctx context.Context, existing *workflowstore.Workflow) error {
	cleared, err := d.tasks.ClearWorkflow(ctx, existing.ID)
	if err != nil || !cleared {
		return err
	}

	schedules, err := d.schedules.ListInWorkflow(ctx, existing.ID)
	if err != nil {
		return err
	}

	for _, schedule := range schedules {
		if err := d.schedules.Delete(ctx, schedule.ID); err != nil {
			return err
		}
	}

	if err := d.workflows.Delete(ctx, existing.ID); err != nil {
		return fmt.Errorf("failed to delete workflow %q: %w", existing.Name, err)
	}

	return nil
}
