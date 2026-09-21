// Package run records one row for every script weave fires against an agent, so a hook that
// failed is a fact on the agent rather than a line in a log file nobody knows to open. The
// output stays on disk and the row says where to find it.
package run

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
)

type RunStore struct {
	queries *models.Queries
}

// NewRunStore builds a run store over db, either the pool or a transaction.
func NewRunStore(db store.DBTX) *RunStore {
	return &RunStore{queries: models.New(db)}
}

type BeginParams struct {
	AgentID   string
	Trigger   string
	Script    string
	LogNumber *int32
}

// Begin opens a row for a run about to start and returns its id.
func (s *RunStore) Begin(ctx context.Context, params BeginParams) (string, error) {
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
func (s *RunStore) Finish(ctx context.Context, params FinishParams) error {
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

// Abandon drops the row of a run that never started.
func (s *RunStore) Abandon(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.DeleteAgentRun(ctx, id); err != nil {
		return fmt.Errorf("failed to drop the record of run %q: %w", id, err)
	}

	return nil
}

// Prune drops an agent's runs past the newest keep, held to the same bound as the log files.
func (s *RunStore) Prune(ctx context.Context, agentID string, keep int) error {
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

// MarkRead claims every failed run an agent is holding.
func (s *RunStore) MarkRead(ctx context.Context, agentID string) error {
	if !store.IsID(agentID) {
		return nil
	}

	if err := s.queries.MarkAgentRunsRead(ctx, agentID); err != nil {
		return fmt.Errorf("failed to claim the failed runs of agent %q: %w", agentID, err)
	}

	return nil
}
