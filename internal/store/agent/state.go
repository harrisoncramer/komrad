package agent

import (
	"context"
	"fmt"
	"time"

	"weave/db/models"
	"weave/internal/store"
)

// ClaimNudge takes the right to tell an agent about waiting mail, checking and stamping in
// one statement so two passes racing cannot both go on to type.
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

// SetStateByID records the state a reconcile pass observed, stamping when it was seen whether or
// not the value moved, and dropping what the agent's CLI reported it was doing when it did.
func (s *Store) SetStateByID(ctx context.Context, id string, state State) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	params := models.SetAgentStateByIDParams{ID: id, State: models.AgentState(state)}
	if err := s.queries.SetAgentStateByID(ctx, params); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// SetViewed claims an agent's conversation as read up to the last turn weave watched end,
// stamping that rather than the clock so a turn landing mid-look is not read with it, or puts
// the mark back when viewed is unset.
func (s *Store) SetViewed(ctx context.Context, id string, viewed bool) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
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
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
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
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	params := models.SetAgentReportedReasonByIDParams{ID: id, ReportedReason: reason}
	if err := s.queries.SetAgentReportedReasonByID(ctx, params); err != nil {
		return fmt.Errorf("failed to set the reported reason for id %q: %w", id, err)
	}

	return nil
}

// ListUnseenTurns reports the agents that have ended a turn the person has not looked at since,
// leaving off one that has gone back to work, since a row going again is not waiting on a reader
// and the turn it is in the middle of will end with a mark of its own.
func (s *Store) ListUnseenTurns(ctx context.Context) ([]string, error) {
	rows, err := s.queries.ListAgentsWithUnseenTurns(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents holding unseen turns: %w", err)
	}

	return rows, nil
}

// MarkSettingUpIfLive says a resume is in flight on one agent and reports whether it did,
// refusing an archived row so the resume notices rather than writing a pane onto a hidden one.
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

// SetBranchByID records the branch checked out at an agent's cwd, or clears it when branch
// is nil, and is written only by the cwd syncer's periodic pass.
func (s *Store) SetBranchByID(ctx context.Context, id string, branch *string) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
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
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetAgentWorktreeRemovedByID(ctx, models.SetAgentWorktreeRemovedByIDParams{ID: id, WorktreeRemoved: removed}); err != nil {
		return fmt.Errorf("failed to set worktree_removed for id %q: %w", id, err)
	}

	return nil
}

// SetInterruptedAtByID records when an agent's conversation was cut off part-way through a
// turn, which a resume reads to decide whether to say anything on the way back in.
func (s *Store) SetInterruptedAtByID(ctx context.Context, id string, at *time.Time) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetAgentInterruptedAtByID(ctx, models.SetAgentInterruptedAtByIDParams{ID: id, InterruptedAt: at}); err != nil {
		return fmt.Errorf("failed to set interrupted_at for id %q: %w", id, err)
	}

	return nil
}

// SetSessionID records an agent's conversation when none is already stored.
func (s *Store) SetSessionID(ctx context.Context, id, sessionID string) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
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

	// The write is guarded on UpdatedAt rather than on the pane, since a tmux server hands out
	// pane ids from a counter that restarts with it and a restart is when weave resumes every
	// agent at once.
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
