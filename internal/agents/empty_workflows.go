package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrade/internal/store"

	agentstore "komrade/internal/store/agent"
	schedulestore "komrade/internal/store/schedule"
	workflowstore "komrade/internal/store/workflow"
)

type emptyWorkflowGetDeleter interface {
	GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error)
	Delete(ctx context.Context, id string) error
}

type workflowResidentLister interface {
	ListArchivedInWorkflow(ctx context.Context, workflowID string) ([]agentstore.Agent, error)
	ListNamesHoldingSchedule(ctx context.Context, scheduleID, workflowID string) ([]string, error)
}

type workflowScheduleListDeleter interface {
	ListInWorkflow(ctx context.Context, workflowID string) ([]schedulestore.Schedule, error)
	Delete(ctx context.Context, id string) error
}

type NewEmptyWorkflowDeleterParams struct {
	Workflows emptyWorkflowGetDeleter
	Agents    workflowResidentLister
	Schedules workflowScheduleListDeleter
}

// EmptyWorkflowDeleter removes an archived workflow once nothing is left inside it, taking the
// archived schedules pointing at it with the row and deleting no agents itself.
type EmptyWorkflowDeleter struct {
	workflows emptyWorkflowGetDeleter
	agents    workflowResidentLister
	schedules workflowScheduleListDeleter
}

// NewEmptyWorkflowDeleter builds the deleter over the three stores a workflow's removal touches.
func NewEmptyWorkflowDeleter(params NewEmptyWorkflowDeleterParams) *EmptyWorkflowDeleter {
	return &EmptyWorkflowDeleter{
		workflows: params.Workflows,
		agents:    params.Agents,
		schedules: params.Schedules,
	}
}

// DeleteIfEmpty permanently removes a workflow once it is archived and holds no agents at all.
func (d *EmptyWorkflowDeleter) DeleteIfEmpty(ctx context.Context, id string) error {
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

	schedules, err := d.heldSchedules(ctx, existing)
	if err != nil {
		return err
	}

	return d.deleteWithSchedules(ctx, existing, schedules)
}

// heldSchedules returns the archived schedules pointing at a workflow, refusing if any is still
// held by an agent outside it.
func (d *EmptyWorkflowDeleter) heldSchedules(ctx context.Context, existing *workflowstore.Workflow) ([]schedulestore.Schedule, error) {
	schedules, err := d.schedules.ListInWorkflow(ctx, existing.ID)
	if err != nil {
		return nil, err
	}

	for _, schedule := range schedules {
		held, err := d.agents.ListNamesHoldingSchedule(ctx, schedule.ID, existing.ID)
		if err != nil {
			return nil, err
		}
		if len(held) > 0 {
			return nil, fmt.Errorf("%w: %q is still held by %s", ErrScheduleHasAgents, schedule.Name, strings.Join(held, ", "))
		}
	}

	return schedules, nil
}

func (d *EmptyWorkflowDeleter) deleteWithSchedules(ctx context.Context, existing *workflowstore.Workflow, schedules []schedulestore.Schedule) error {
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
