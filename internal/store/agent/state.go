package agent

import (
	"context"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/internal/store"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/uuid"
)

// SetStateByID records the state a reconcile pass observed, stamping when it was seen whether
// or not the value moved, and clearing the reported reason when it did.
func (s *AgentStore) SetStateByID(ctx context.Context, id uuid.UUID, state agentv1.AgentState) error {
	stored, err := fromServiceToDBAgentState(state)
	if err != nil {
		return err
	}

	params := models.SetAgentStateByIDParams{ID: id, State: stored}
	if err := s.queries.SetAgentStateByID(ctx, params); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// SetTurnEnded records that a turn of an agent's ended, written either from the agent's own CLI
// reporting it or, when nothing did, from the reconciler watching the pane stop working.
func (s *AgentStore) SetTurnEnded(ctx context.Context, id uuid.UUID) error {
	if err := s.queries.SetAgentTurnEndedByID(ctx, id); err != nil {
		return fmt.Errorf("failed to set the turn end for id %q: %w", id, err)
	}

	return nil
}

// SetTurnFailed records that an API error ended a turn of an agent's, with what its CLI printed.
func (s *AgentStore) SetTurnFailed(ctx context.Context, id uuid.UUID, turnError string) error {
	params := models.SetAgentTurnFailedByIDParams{ID: id, TurnError: turnError}
	if err := s.queries.SetAgentTurnFailedByID(ctx, params); err != nil {
		return fmt.Errorf("failed to set the turn failure for id %q: %w", id, err)
	}

	return nil
}

// SetReportedReason records what an agent's own CLI last reported it was doing, an empty reason
// clearing whatever was reported before it.
func (s *AgentStore) SetReportedReason(ctx context.Context, id uuid.UUID, reason string) error {
	params := models.SetAgentReportedReasonByIDParams{ID: id, ReportedReason: reason}
	updated, err := s.queries.SetAgentReportedReasonByID(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to set the reported reason for id %q: %w", id, err)
	}

	if updated == 0 {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	return nil
}

// MarkSettingUpIfLive says a resume is in flight on one agent and reports whether it did,
// refusing an archived row.
func (s *AgentStore) MarkSettingUpIfLive(ctx context.Context, id uuid.UUID) (bool, error) {
	marked, err := s.queries.MarkAgentSettingUpIfLive(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark id %q as setting up: %w", id, err)
	}

	return len(marked) > 0, nil
}

// MarkSettingUpIfFailedToCreate moves a failed-to-create agent into setting_up, reporting
// whether it did.
func (s *AgentStore) MarkSettingUpIfFailedToCreate(ctx context.Context, id uuid.UUID) (bool, error) {
	marked, err := s.queries.MarkAgentSettingUpIfFailedToCreate(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark id %q as setting up for retry: %w", id, err)
	}

	return len(marked) > 0, nil
}

// SetInterruptedAtByID records when an agent's conversation was cut off part-way through a
// turn.
func (s *AgentStore) SetInterruptedAtByID(ctx context.Context, id uuid.UUID, at *time.Time) error {
	if err := s.queries.SetAgentInterruptedAtByID(ctx, models.SetAgentInterruptedAtByIDParams{ID: id, InterruptedAt: at}); err != nil {
		return fmt.Errorf("failed to set interrupted_at for id %q: %w", id, err)
	}

	return nil
}

// SetHooksByID records the hooks an agent's latest launch installed.
func (s *AgentStore) SetHooksByID(ctx context.Context, id uuid.UUID, hooks []string) error {
	if hooks == nil {
		hooks = []string{}
	}
	if err := s.queries.SetAgentHooksByID(ctx, models.SetAgentHooksByIDParams{ID: id, Hooks: hooks}); err != nil {
		return fmt.Errorf("failed to set hooks for id %q: %w", id, err)
	}

	return nil
}

type SetHarnessParams struct {
	ID        uuid.UUID
	Harness   string
	Flags     string
	SessionID string
}

// SetHarness moves an agent onto another CLI, with the flags and the conversation that belong
// to it.
func (s *AgentStore) SetHarness(ctx context.Context, params SetHarnessParams) error {
	if err := s.queries.SetAgentHarness(ctx, models.SetAgentHarnessParams{
		ID:        params.ID,
		Harness:   params.Harness,
		Flags:     params.Flags,
		SessionID: params.SessionID,
	}); err != nil {
		return fmt.Errorf("failed to set the harness for id %q: %w", params.ID, err)
	}

	return nil
}

// SetSessionID records an agent's conversation when none is already stored and the agent has not
// held it before.
func (s *AgentStore) SetSessionID(ctx context.Context, id uuid.UUID, sessionID string) error {
	if err := s.queries.SetAgentSessionID(ctx, models.SetAgentSessionIDParams{ID: id, SessionID: sessionID}); err != nil {
		return fmt.Errorf("failed to set the session for id %q: %w", id, err)
	}

	return nil
}

// SetAlive records whether each of the given agents still owns its pane.
func (s *AgentStore) SetAlive(ctx context.Context, ids []uuid.UUID, alive bool) error {
	if len(ids) == 0 {
		return nil
	}

	if err := s.queries.SetAgentsAlive(ctx, models.SetAgentsAliveParams{Ids: ids, Alive: alive}); err != nil {
		return fmt.Errorf("failed to set %d agents alive=%t: %w", len(ids), alive, err)
	}

	return nil
}

type ForgetPaneParams struct {
	ID            uuid.UUID
	Pane          string
	UpdatedAt     time.Time
	InterruptedAt *time.Time
}

// ForgetPane drops an agent's pane, recording an interrupted turn in the same write when
// InterruptedAt is set, and reports whether it did.
func (s *AgentStore) ForgetPane(ctx context.Context, params ForgetPaneParams) (bool, error) {
	// The write checks UpdatedAt rather than the pane, whose ids come from a counter
	// that restarts with the tmux server.
	forgotten, err := s.queries.ForgetAgentPane(ctx, models.ForgetAgentPaneParams{
		ID:            params.ID,
		PaneID:        params.Pane,
		UpdatedAt:     params.UpdatedAt,
		InterruptedAt: params.InterruptedAt,
	})
	if err != nil {
		return false, fmt.Errorf("failed to forget the pane for id %q: %w", params.ID, err)
	}

	return len(forgotten) > 0, nil
}
