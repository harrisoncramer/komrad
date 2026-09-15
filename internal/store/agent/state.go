package agent

import (
	"context"
	"fmt"
	"time"

	"weave/db/models"
	"weave/internal/store"
)

// ClaimNudge takes the right to tell an agent about waiting mail, checking and stamping in
// one statement.
func (s *Store) ClaimNudge(ctx context.Context, id string, retryAfter time.Duration) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	claimed, err := s.queries.ClaimAgentNudge(ctx, models.ClaimAgentNudgeParams{
		ID:          id,
		RetryMicros: retryAfter.Microseconds(),
	})
	if err != nil {
		return false, fmt.Errorf("failed to claim the nudge for agent %q: %w", id, err)
	}

	return len(claimed) > 0, nil
}

// SetNudgeSent records that an agent was actually told about its mail, an advisory stamp the
// claim alone does not make.
func (s *Store) SetNudgeSent(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetAgentNudgeSentByID(ctx, id); err != nil {
		return fmt.Errorf("failed to record the nudge sent to agent %q: %w", id, err)
	}

	return nil
}

// SetStateByID records the state a reconcile pass observed, stamping when it was seen whether
// or not the value moved, and clearing the reported reason when it did.
func (s *Store) SetStateByID(ctx context.Context, id string, state State) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	params := models.SetAgentStateByIDParams{ID: id, State: models.AgentState(state)}
	if err := s.queries.SetAgentStateByID(ctx, params); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// SetViewed claims an agent's conversation as read up to the last turn weave watched end, or
// puts the mark back when viewed is unset.
func (s *Store) SetViewed(ctx context.Context, id string, viewed bool) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentViewedByID(ctx, models.SetAgentViewedByIDParams{ID: id, Viewed: viewed}); err != nil {
		return fmt.Errorf("failed to set viewed for id %q: %w", id, err)
	}

	return nil
}

// SetTurnEnded records that a turn of an agent's ended, written either from the agent's own CLI
// reporting it or, when nothing did, from the reconciler watching the pane stop working.
func (s *Store) SetTurnEnded(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentTurnEndedByID(ctx, id); err != nil {
		return fmt.Errorf("failed to set the turn end for id %q: %w", id, err)
	}

	return nil
}

// SetReportedReason records what an agent's own CLI last reported it was doing, an empty reason
// clearing whatever was reported before it.
func (s *Store) SetReportedReason(ctx context.Context, id, reason string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	params := models.SetAgentReportedReasonByIDParams{ID: id, ReportedReason: reason}
	if err := s.queries.SetAgentReportedReasonByID(ctx, params); err != nil {
		return fmt.Errorf("failed to set the reported reason for id %q: %w", id, err)
	}

	return nil
}

// ListUnseenTurns reports the agents that have ended a turn the person has not looked at since,
// leaving off one that has gone back to work.
func (s *Store) ListUnseenTurns(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListAgentsWithUnseenTurns(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents holding unseen turns: %w", err)
	}

	return rows, nil
}

// MarkSettingUpIfLive says a resume is in flight on one agent and reports whether it did,
// refusing an archived row.
func (s *Store) MarkSettingUpIfLive(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	marked, err := s.queries.MarkAgentSettingUpIfLive(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark id %q as setting up: %w", id, err)
	}

	return len(marked) > 0, nil
}

// MarkSettingUpIfFailedToCreate moves a failed-to-create agent into setting_up, reporting
// whether it did.
func (s *Store) MarkSettingUpIfFailedToCreate(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	marked, err := s.queries.MarkAgentSettingUpIfFailedToCreate(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark id %q as setting up for retry: %w", id, err)
	}

	return len(marked) > 0, nil
}

// SetBranchByID records the branch checked out at an agent's cwd, or clears it when branch
// is nil, and is written only by the cwd syncer's periodic pass.
func (s *Store) SetBranchByID(ctx context.Context, id string, branch *string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentBranchByID(ctx, models.SetAgentBranchByIDParams{ID: id, Branch: branch}); err != nil {
		return fmt.Errorf("failed to set the branch for id %q: %w", id, err)
	}

	return nil
}

// SetWorktreeRemovedByID records whether an agent's cwd no longer exists on disk, written
// only by the cwd syncer's periodic pass.
func (s *Store) SetWorktreeRemovedByID(ctx context.Context, id string, removed bool) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentWorktreeRemovedByID(ctx, models.SetAgentWorktreeRemovedByIDParams{ID: id, WorktreeRemoved: removed}); err != nil {
		return fmt.Errorf("failed to set worktree_removed for id %q: %w", id, err)
	}

	return nil
}

// SetSequenceInProgressByID records whether a merge, a rebase or a cherry-pick is part-way
// through an agent's checkout.
func (s *Store) SetSequenceInProgressByID(ctx context.Context, id string, inProgress bool) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetAgentSequenceInProgressByID(ctx, models.SetAgentSequenceInProgressByIDParams{ID: id, SequenceInProgress: inProgress}); err != nil {
		return fmt.Errorf("failed to set sequence_in_progress for id %q: %w", id, err)
	}

	return nil
}

// SetInterruptedAtByID records when an agent's conversation was cut off part-way through a
// turn.
func (s *Store) SetInterruptedAtByID(ctx context.Context, id string, at *time.Time) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentInterruptedAtByID(ctx, models.SetAgentInterruptedAtByIDParams{ID: id, InterruptedAt: at}); err != nil {
		return fmt.Errorf("failed to set interrupted_at for id %q: %w", id, err)
	}

	return nil
}

// Hold records that a person has stopped an agent, and reports false when somebody already had it.
func (s *Store) Hold(ctx context.Context, params HoldParams) (bool, error) {
	if !store.IsID(params.ID) {
		return false, fmt.Errorf("%w: agent %q", store.ErrNotFound, params.ID)
	}

	held, err := s.queries.HoldAgentByID(ctx, models.HoldAgentByIDParams{
		ID:          params.ID,
		HeldAt:      &params.At,
		HeldMidTurn: params.MidTurn,
	})
	if err != nil {
		return false, fmt.Errorf("failed to hold agent %q: %w", params.ID, err)
	}

	return len(held) > 0, nil
}

// HoldParams is the agent being held and what holding it interrupted.
type HoldParams struct {
	ID string
	At time.Time
	// MidTurn says the interrupt cut a turn that was still running.
	MidTurn bool
}

// Release lets go of an agent, and reports false when nobody was holding it.
func (s *Store) Release(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	released, err := s.queries.ReleaseAgentByID(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to release agent %q: %w", id, err)
	}

	return len(released) > 0, nil
}

// SetSessionID records an agent's conversation when none is already stored.
func (s *Store) SetSessionID(ctx context.Context, id, sessionID string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentSessionID(ctx, models.SetAgentSessionIDParams{ID: id, SessionID: sessionID}); err != nil {
		return fmt.Errorf("failed to set the session for id %q: %w", id, err)
	}

	return nil
}

type ForgetPaneParams struct {
	ID            string
	Pane          string
	UpdatedAt     time.Time
	InterruptedAt *time.Time
}

// ForgetPane drops an agent's pane, recording an interrupted turn in the same write when
// InterruptedAt is set, and reports whether it did.
func (s *Store) ForgetPane(ctx context.Context, params ForgetPaneParams) (bool, error) {
	if !store.IsID(params.ID) {
		return false, nil
	}

	// The write is guarded on UpdatedAt rather than on the pane, whose ids come from a counter
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
