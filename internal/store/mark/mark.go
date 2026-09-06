// Package mark stores the letters a person addresses rows by, which are held by an agent or a
// workflow and are the panel's own rather than anything an agent writes.
package mark

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
)

// Mark is one letter and the row it names, exactly one of which is set.
type Mark struct {
	Mark       string
	AgentID    *string
	WorkflowID *string
}

type Store struct {
	queries *models.Queries
}

// New builds a mark store over a pool or transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// List reads every mark, which is a whole table small enough that the panel takes all of it.
func (s *Store) List(ctx context.Context) ([]Mark, error) {
	rows, err := s.queries.ListMarks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list marks: %w", err)
	}

	marks := make([]Mark, 0, len(rows))
	for _, row := range rows {
		marks = append(marks, Mark{
			Mark:       row.Mark,
			AgentID:    row.AgentID,
			WorkflowID: row.WorkflowID,
		})
	}

	return marks, nil
}

// ClearConflicts frees the letter and whatever the row already answered to, which is what
// claiming either has to do first.
func (s *Store) ClearConflicts(ctx context.Context, mark string, target Target) error {
	err := s.queries.ClearMarkConflicts(ctx, models.ClearMarkConflictsParams{
		Mark:       mark,
		AgentID:    target.AgentID,
		WorkflowID: target.WorkflowID,
	})
	if err != nil {
		return fmt.Errorf("failed to clear mark %q: %w", mark, err)
	}

	return nil
}

// Clear takes whatever letter a row answers to off it, and reports whether it held one.
func (s *Store) Clear(ctx context.Context, target Target) (bool, error) {
	cleared, err := s.queries.DeleteMarkForTarget(ctx, models.DeleteMarkForTargetParams{
		AgentID:    target.AgentID,
		WorkflowID: target.WorkflowID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to clear the mark on %s: %w", target, err)
	}

	return cleared > 0, nil
}

// Set puts a letter on a live row and reports whether the row was there to take it, with the
// caller having freed what stood in the way first.
func (s *Store) Set(ctx context.Context, mark string, target Target) (bool, error) {
	var marked int64
	var err error

	switch {
	case target.AgentID != nil:
		marked, err = s.queries.InsertAgentMark(ctx, models.InsertAgentMarkParams{Mark: mark, AgentID: *target.AgentID})
	case target.WorkflowID != nil:
		marked, err = s.queries.InsertWorkflowMark(ctx, models.InsertWorkflowMarkParams{Mark: mark, WorkflowID: *target.WorkflowID})
	}
	if err != nil {
		return false, fmt.Errorf("failed to put mark %q on %s: %w", mark, target, err)
	}

	return marked > 0, nil
}

// Target is the row a mark names, which is an agent or a workflow and never both.
type Target struct {
	AgentID    *string
	WorkflowID *string
}

// OnAgent is the target naming one agent.
func OnAgent(id string) Target {
	return Target{AgentID: &id}
}

// OnWorkflow is the target naming one workflow.
func OnWorkflow(id string) Target {
	return Target{WorkflowID: &id}
}

// IsID reports whether the target names a row the store could hold a mark for.
func (t Target) IsID() bool {
	switch {
	case t.AgentID != nil:
		return store.IsID(*t.AgentID)
	case t.WorkflowID != nil:
		return store.IsID(*t.WorkflowID)
	}

	return false
}

// String names the row a target points at, for the errors that have to say which one.
func (t Target) String() string {
	switch {
	case t.AgentID != nil:
		return "agent " + *t.AgentID
	case t.WorkflowID != nil:
		return "workflow " + *t.WorkflowID
	}

	return "nothing"
}
