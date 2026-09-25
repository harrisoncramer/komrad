package agents

import (
	"context"
	"fmt"
)

type rowWriteStore interface {
	SetHidden(ctx context.Context, id string, hidden bool) (bool, error)
	Rename(ctx context.Context, id, name string) (bool, error)
	SetReportedReason(ctx context.Context, id, reason string) error
	SetTurnEnded(ctx context.Context, id string) error
}

type runMarker interface {
	MarkRead(ctx context.Context, agentID string) error
}

// turnRecorder is where what an agent's own CLI reports about itself reaches the reconcile loop.
type turnRecorder interface {
	RecordTurnBoundary(agentID string)
	RecordReportedReason(agentID, reason string)
}

// NewRowWriterParams is what a row writer is built over.
type NewRowWriterParams struct {
	Agents     rowWriteStore
	Runs       runMarker
	Reconciler turnRecorder
}

// RowWriter changes what an agent's row says without moving the agent through its life.
type RowWriter struct {
	agents     rowWriteStore
	runs       runMarker
	reconciler turnRecorder
}

// NewRowWriter builds the writer over the store, the record of runs and the reconciler.
func NewRowWriter(params NewRowWriterParams) *RowWriter {
	return &RowWriter{
		agents:     params.Agents,
		runs:       params.Runs,
		reconciler: params.Reconciler,
	}
}

// SetHidden hides an agent from the panel's sidebar or brings it back.
func (w *RowWriter) SetHidden(ctx context.Context, id string, hidden bool) error {
	changed, err := w.agents.SetHidden(ctx, id, hidden)
	if err != nil {
		return fmt.Errorf("failed to hide agent %q: %w", id, err)
	}
	if !changed {
		return ErrAgentNotFound.Saying("no agent with id %q", id)
	}

	return nil
}

// Rename gives an agent a new display name, refusing one another agent in its workflow holds.
func (w *RowWriter) Rename(ctx context.Context, id, name string) error {
	renamed, err := w.agents.Rename(ctx, id, name)
	if err != nil {
		return fmt.Errorf("failed to rename agent %q: %w", id, err)
	}
	if !renamed {
		return ErrAgentNotFound.Saying("no agent with id %q", id)
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
}

// RecordReport puts the reason beside the state the pane decided and stamps a reported turn end,
// telling the reconciler both before either reaches the store.
func (w *RowWriter) RecordReport(ctx context.Context, params RecordReportParams) error {
	// Both stamps are taken before either write, so a store that is down costs the flags it
	// would have raised rather than also costing the reconciler its shortcut to idle.
	w.reconciler.RecordReportedReason(params.AgentID, params.Reason)
	if params.TurnEnded {
		w.reconciler.RecordTurnBoundary(params.AgentID)
	}

	if err := w.agents.SetReportedReason(ctx, params.AgentID, params.Reason); err != nil {
		return fmt.Errorf("failed to record what agent %q reported it was doing: %w", params.AgentID, err)
	}

	if !params.TurnEnded {
		return nil
	}

	if err := w.agents.SetTurnEnded(ctx, params.AgentID); err != nil {
		return fmt.Errorf("failed to record the turn end of agent %q: %w", params.AgentID, err)
	}

	return nil
}
