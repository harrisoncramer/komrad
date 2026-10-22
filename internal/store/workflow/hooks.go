package workflow

import (
	"context"
	"fmt"

	"komrad/config"
	"komrad/db/models"
	"komrad/lib/xslice"

	"github.com/google/uuid"
)

// The moments a workflow can hang a script on, each named for the event it hangs on and the
// side of it that runs, so reading the name says when it fires.
const (
	HookAgentPreLaunch   = config.HookAgentPreLaunch
	HookAgentPostLaunch  = config.HookAgentPostLaunch
	HookAgentPreResume   = config.HookAgentPreResume
	HookAgentPostResume  = config.HookAgentPostResume
	HookAgentPreArchive  = config.HookAgentPreArchive
	HookAgentPostArchive = config.HookAgentPostArchive
	HookAgentPreRestore  = config.HookAgentPreRestore
	HookAgentPostRestore = config.HookAgentPostRestore
	HookAgentPreDelete   = config.HookAgentPreDelete
	HookAgentPostDelete  = config.HookAgentPostDelete
	HookTaskPreOpen      = config.HookTaskPreOpen
	HookTaskPostArchive  = config.HookTaskPostArchive
	HookTaskPreDelete    = config.HookTaskPreDelete
)

// HookEvents are the moments a workflow can hang a script on, an agent's in the order they fire and then its task's.
func HookEvents() []string {
	return []string{
		HookAgentPreLaunch,
		HookAgentPostLaunch,
		HookAgentPreResume,
		HookAgentPostResume,
		HookAgentPreArchive,
		HookAgentPostArchive,
		HookAgentPreRestore,
		HookAgentPostRestore,
		HookAgentPreDelete,
		HookAgentPostDelete,
		HookTaskPreOpen,
		HookTaskPostArchive,
		HookTaskPreDelete,
	}
}

// IsTaskEvent reports whether a moment is in a task's life rather than an agent's.
func IsTaskEvent(event string) bool {
	return event == HookTaskPreOpen || event == HookTaskPostArchive || event == HookTaskPreDelete
}

// Hook is one script a workflow hangs on a moment, and a moment can hold several.
type Hook struct {
	Event  string
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
			Event:      hook.Event,
			Script:     hook.Script,
			Position:   int32(position),
		}); err != nil {
			return fmt.Errorf("failed to add the %s hook %q to workflow %q: %w", hook.Event, hook.Script, workflowID, err)
		}
	}

	return nil
}

func (s *WorkflowStore) hooksFor(ctx context.Context, workflowID uuid.UUID) ([]Hook, error) {
	rows, err := s.queries.ListHooksForWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the hooks of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(rows, func(row *models.ListHooksForWorkflowRow) Hook {
		return Hook{Event: row.Event, Script: row.Script}
	}), nil
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
		hooks[row.WorkflowID] = append(hooks[row.WorkflowID], Hook{Event: row.Event, Script: row.Script})
	}

	return hooks, nil
}
