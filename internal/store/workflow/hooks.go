package workflow

import (
	"context"
	"fmt"
	"slices"

	"komrad/config"
	"komrad/db/models"
	"komrad/lib/protoenum"
	eventv1 "komrad/proto/gen/event/v1"

	"github.com/google/uuid"
)

// IsTaskMoment reports whether a moment is in a task's life rather than an agent's.
func IsTaskMoment(moment eventv1.Moment) bool {
	return slices.Contains([]eventv1.Moment{eventv1.Moment_MOMENT_TASK_PRE_OPEN, eventv1.Moment_MOMENT_TASK_POST_ARCHIVE, eventv1.Moment_MOMENT_TASK_PRE_DELETE}, moment)
}

// Hook is one script a workflow hangs on a moment, and a moment can hold several.
type Hook struct {
	Event  eventv1.Moment
	Script string
}

// setHooks rewrites a workflow's hooks from the whole list, keeping the order they were sent
// in.
func (s *WorkflowStore) setHooks(ctx context.Context, workflowID uuid.UUID, hooks []Hook) error {
	if err := s.queries.DeleteWorkflowHooks(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to clear hooks for workflow %q: %w", workflowID, err)
	}

	for position, hook := range hooks {
		if err := s.queries.InsertWorkflowHook(ctx, models.InsertWorkflowHookParams{
			WorkflowID: workflowID,
			Event:      protoenum.Name(hook.Event),
			Script:     hook.Script,
			Position:   int32(position),
		}); err != nil {
			return fmt.Errorf("failed to add the %s hook %q to workflow %q: %w", protoenum.Name(hook.Event), hook.Script, workflowID, err)
		}
	}

	return nil
}

func (s *WorkflowStore) hooksFor(ctx context.Context, workflowID uuid.UUID) ([]Hook, error) {
	rows, err := s.queries.ListHooksForWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the hooks of workflow %q: %w", workflowID, err)
	}

	hooks := make([]Hook, 0, len(rows))
	for _, row := range rows {
		hook, err := hookFromRow(row.Event, row.Script)
		if err != nil {
			return nil, fmt.Errorf("failed to read a hook of workflow %q: %w", workflowID, err)
		}
		hooks = append(hooks, *hook)
	}

	return hooks, nil
}

// hooksForWorkflows reads the hooks of the workflows named.
func (s *WorkflowStore) hooksForWorkflows(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]Hook, error) {
	if len(ids) == 0 {
		return map[uuid.UUID][]Hook{}, nil
	}

	rows, err := s.queries.ListHooksForWorkflows(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to read the hooks of %d workflows: %w", len(ids), err)
	}

	hooks := make(map[uuid.UUID][]Hook)
	for _, row := range rows {
		hook, err := hookFromRow(row.Event, row.Script)
		if err != nil {
			return nil, fmt.Errorf("failed to read a hook of workflow %q: %w", row.WorkflowID, err)
		}
		hooks[row.WorkflowID] = append(hooks[row.WorkflowID], *hook)
	}

	return hooks, nil
}

// hookFromRow reads a stored hook, failing on a moment komrad runs no hook at.
func hookFromRow(event, script string) (*Hook, error) {
	moment, err := protoenum.Parse[eventv1.Moment](event)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(config.HookMoments(), moment) {
		return nil, fmt.Errorf("komrad runs no hook at %s", event)
	}

	return &Hook{Event: moment, Script: script}, nil
}
