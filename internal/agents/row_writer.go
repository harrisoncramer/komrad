package agents

import (
	"context"
	"fmt"

	"komrad/internal/agenterr"
	agentstore "komrad/internal/store/agent"
	runstore "komrad/internal/store/run"
	"komrad/internal/wake"
)

// NewRowWriterParams is what a row writer is built over.
type NewRowWriterParams struct {
	Agents         *agentstore.AgentStore
	Runs           *runstore.RunStore
	CwdSync        wake.Waker
	TranscriptSync wake.Waker
}

// RowWriter changes what an agent's row says without moving the agent through its life.
type RowWriter struct {
	agents         *agentstore.AgentStore
	runs           *runstore.RunStore
	cwdSync        wake.Waker
	transcriptSync wake.Waker
}

// NewRowWriter builds the writer over the store, the record of runs and the wakers of the loops
// that read what a turn left behind.
func NewRowWriter(params NewRowWriterParams) *RowWriter {
	return &RowWriter{
		agents:         params.Agents,
		runs:           params.Runs,
		cwdSync:        params.CwdSync,
		transcriptSync: params.TranscriptSync,
	}
}

// Rename gives an agent a new display name, refusing one another agent in its workflow holds.
func (w *RowWriter) Rename(ctx context.Context, id, name string) error {
	renamed, err := w.agents.Rename(ctx, id, name)
	if err != nil {
		return fmt.Errorf("failed to rename agent %q: %w", id, err)
	}
	if !renamed {
		return agenterr.ErrAgentNotFound.Saying("no agent with id %q", id)
	}

	return nil
}

// MarkRunsRead claims the failed runs an agent is holding.
func (w *RowWriter) MarkRunsRead(ctx context.Context, id string) error {
	if err := w.runs.MarkRead(ctx, id); err != nil {
		return fmt.Errorf("failed to claim the failed runs of agent %q: %w", id, err)
	}

	return nil
}

// RecordReportParams is what an agent's own CLI read out of one of its hook payloads.
type RecordReportParams struct {
	AgentID   string
	Reason    string
	TurnEnded bool
	TurnError string
}

// RecordReport puts the reason beside the state the pane decided, stamps a reported turn end, and
// wakes the cwd and transcript syncs once a turn ends.
func (w *RowWriter) RecordReport(ctx context.Context, params RecordReportParams) error {
	if params.TurnEnded {
		w.cwdSync.Wake()
		w.transcriptSync.Wake()
	}

	if err := w.agents.SetReportedReason(ctx, params.AgentID, params.Reason); err != nil {
		return fmt.Errorf("failed to record what agent %q reported it was doing: %w", params.AgentID, err)
	}

	if !params.TurnEnded {
		return nil
	}

	if params.TurnError != "" {
		if err := w.agents.SetTurnFailed(ctx, params.AgentID, params.TurnError); err != nil {
			return fmt.Errorf("failed to record the failed turn of agent %q: %w", params.AgentID, err)
		}

		return nil
	}

	if err := w.agents.SetTurnEnded(ctx, params.AgentID); err != nil {
		return fmt.Errorf("failed to record the turn end of agent %q: %w", params.AgentID, err)
	}

	return nil
}
