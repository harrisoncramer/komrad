package workflow

import (
	"context"
	"fmt"

	"weave/config"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

// The moments a workflow can hang a script on, each named for the event it hangs on and the
// side of it that runs, so reading the name says when it fires.
const (
	HookPreLaunch   = config.HookPreLaunch
	HookPostLaunch  = config.HookPostLaunch
	HookPostResume  = config.HookPostResume
	HookPostRestore = config.HookPostRestore
	HookPreTeardown = config.HookPreTeardown
)

// HookEvents are the moments a workflow can hang a script on, in the order they fire.
func HookEvents() []string {
	return []string{HookPreLaunch, HookPostLaunch, HookPostResume, HookPostRestore, HookPreTeardown}
}

// Hook is one script a workflow hangs on a moment, and a moment can hold several.
type Hook struct {
	Event  string
	Script string
}

// setHooks rewrites a workflow's hooks from the whole list, keeping the order they were sent
// in.
func (s *WorkflowStore) setHooks(ctx context.Context, workflowID string, hooks []Hook) error {
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

	s.db.Touch(store.TableWorkflows, workflowID)

	return nil
}

func (s *WorkflowStore) hooksFor(ctx context.Context, workflowID string) ([]Hook, error) {
	rows, err := s.queries.ListHooksForWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the hooks of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(rows, func(row *models.ListHooksForWorkflowRow) Hook {
		return Hook{Event: row.Event, Script: row.Script}
	}), nil
}

// hooksByWorkflow reads every workflow's hooks in one query.
func (s *WorkflowStore) hooksByWorkflow(ctx context.Context) (map[string][]Hook, error) {
	rows, err := s.queries.ListWorkflowHooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow hooks: %w", err)
	}

	hooks := make(map[string][]Hook)
	for _, row := range rows {
		hooks[row.WorkflowID] = append(hooks[row.WorkflowID], Hook{Event: row.Event, Script: row.Script})
	}

	return hooks, nil
}

// hooksForWorkflows reads the hooks of the workflows named.
func (s *WorkflowStore) hooksForWorkflows(ctx context.Context, ids []string) (map[string][]Hook, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListHooksForWorkflows(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to read the hooks of %d workflows: %w", len(ids), err)
	}

	hooks := make(map[string][]Hook)
	for _, row := range rows {
		hooks[row.WorkflowID] = append(hooks[row.WorkflowID], Hook{Event: row.Event, Script: row.Script})
	}

	return hooks, nil
}
