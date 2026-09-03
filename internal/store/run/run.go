// Package run records one row for every script weave fires against an agent, so a hook that
// failed is a fact on the agent rather than a line in a log file nobody knows to open. The
// output itself stays on disk; the row is the index that points at it.
package run

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

type Store struct {
	queries *models.Queries
}

// New builds a run store over db, either the pool or a transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

type BeginParams struct {
	AgentID   string
	Trigger   string
	Script    string
	LogNumber *int32
}

// Begin opens a row for a run about to start and returns its id, which Finish is called with.
func (s *Store) Begin(ctx context.Context, params BeginParams) (string, error) {
	if !store.IsID(params.AgentID) {
		return "", fmt.Errorf("failed to record a run of %q: %q is not an agent id", params.Script, params.AgentID)
	}

	id, err := s.queries.InsertAgentRun(ctx, models.InsertAgentRunParams{
		AgentID:   params.AgentID,
		Trigger:   params.Trigger,
		Script:    params.Script,
		LogNumber: params.LogNumber,
	})
	if err != nil {
		return "", fmt.Errorf("failed to record a run of %q for agent %q: %w", params.Script, params.AgentID, err)
	}

	return id, nil
}

type FinishParams struct {
	ID       string
	ExitCode *int32
	Failure  string
}

// Finish closes a run's row with what it exited on and why it failed, if it did.
func (s *Store) Finish(ctx context.Context, params FinishParams) error {
	if !store.IsID(params.ID) {
		return nil
	}

	if err := s.queries.FinishAgentRun(ctx, models.FinishAgentRunParams{
		ID:       params.ID,
		ExitCode: params.ExitCode,
		Failure:  params.Failure,
	}); err != nil {
		return fmt.Errorf("failed to close the record of run %q: %w", params.ID, err)
	}

	return nil
}

// Abandon drops the row of a run that never started, matching the log file weave removes for
// the same reason: a refusal is not a run and should not count against what an agent keeps.
func (s *Store) Abandon(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.DeleteAgentRun(ctx, id); err != nil {
		return fmt.Errorf("failed to drop the record of run %q: %w", id, err)
	}

	return nil
}

// Prune drops an agent's runs past the newest keep, held to the same bound as the log files so
// that a row is never left pointing at output that has already been swept.
func (s *Store) Prune(ctx context.Context, agentID string, keep int) error {
	if !store.IsID(agentID) || keep <= 0 {
		return nil
	}

	if err := s.queries.PruneAgentRuns(ctx, models.PruneAgentRunsParams{
		AgentID: agentID,
		Keep:    int32(keep),
	}); err != nil {
		return fmt.Errorf("failed to prune the runs of agent %q: %w", agentID, err)
	}

	return nil
}

// Failure is one agent's most recent failed run nobody has looked at, and the log its output
// went to, which is what opens the pane on the run that failed rather than on the last one.
type Failure struct {
	AgentID   string
	LogNumber *int32
}

// ListUnreadFailures returns the agents holding a failed run nobody has looked at.
func (s *Store) ListUnreadFailures(ctx context.Context) ([]Failure, error) {
	rows, err := s.queries.ListAgentsWithUnreadFailures(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents holding a failed run: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListAgentsWithUnreadFailuresRow) Failure {
		return Failure{AgentID: row.AgentID, LogNumber: row.LogNumber}
	}), nil
}

// MarkRead claims every failed run an agent is holding, which is what looking at its run log
// does: the mark is about there being something to read rather than about a particular run.
func (s *Store) MarkRead(ctx context.Context, agentID string) error {
	if !store.IsID(agentID) {
		return nil
	}

	if err := s.queries.MarkAgentRunsRead(ctx, agentID); err != nil {
		return fmt.Errorf("failed to claim the failed runs of agent %q: %w", agentID, err)
	}

	return nil
}
