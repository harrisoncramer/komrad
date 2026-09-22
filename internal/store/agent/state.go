package agent

import (
	"context"
	"fmt"
	"time"

	"weave/db/models"
	"weave/internal/git"
	"weave/internal/store"
)

// ClaimNudge takes the right to tell an agent about waiting mail, checking and stamping in
// one statement.
func (s *AgentStore) ClaimNudge(ctx context.Context, id string, retryAfter time.Duration) (bool, error) {
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

	s.db.Touch(store.TableAgents, id)

	return len(claimed) > 0, nil
}

// SetNudgeSent records that an agent was actually told about its mail, an advisory stamp the
// claim alone does not make.
func (s *AgentStore) SetNudgeSent(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetAgentNudgeSentByID(ctx, id); err != nil {
		return fmt.Errorf("failed to record the nudge sent to agent %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return nil
}

// SetStateByID records the state a reconcile pass observed, stamping when it was seen whether
// or not the value moved, and clearing the reported reason when it did.
func (s *AgentStore) SetStateByID(ctx context.Context, id string, state State) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	params := models.SetAgentStateByIDParams{ID: id, State: models.AgentState(state)}
	if err := s.queries.SetAgentStateByID(ctx, params); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return nil
}

// SetTurnEnded records that a turn of an agent's ended, written either from the agent's own CLI
// reporting it or, when nothing did, from the reconciler watching the pane stop working.
func (s *AgentStore) SetTurnEnded(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentTurnEndedByID(ctx, id); err != nil {
		return fmt.Errorf("failed to set the turn end for id %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return nil
}

// SetReportedReason records what an agent's own CLI last reported it was doing, an empty reason
// clearing whatever was reported before it.
func (s *AgentStore) SetReportedReason(ctx context.Context, id, reason string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	params := models.SetAgentReportedReasonByIDParams{ID: id, ReportedReason: reason}
	if err := s.queries.SetAgentReportedReasonByID(ctx, params); err != nil {
		return fmt.Errorf("failed to set the reported reason for id %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return nil
}

// MarkSettingUpIfLive says a resume is in flight on one agent and reports whether it did,
// refusing an archived row.
func (s *AgentStore) MarkSettingUpIfLive(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	marked, err := s.queries.MarkAgentSettingUpIfLive(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark id %q as setting up: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return len(marked) > 0, nil
}

// MarkSettingUpIfFailedToCreate moves a failed-to-create agent into setting_up, reporting
// whether it did.
func (s *AgentStore) MarkSettingUpIfFailedToCreate(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	marked, err := s.queries.MarkAgentSettingUpIfFailedToCreate(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to mark id %q as setting up for retry: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return len(marked) > 0, nil
}

// SetCheckoutParams is what the cwd syncer read of an agent's checkout.
type SetCheckoutParams struct {
	// Branch is the branch checked out at the cwd, nil when git names none.
	Branch *string
	// RepositoryURL is the repository the cwd pushes to, nil when it names none.
	RepositoryURL *string
	// WorktreeRemoved says the cwd no longer exists on disk.
	WorktreeRemoved bool
	// SequenceInProgress says a merge, a rebase or a cherry-pick is part-way through the checkout.
	SequenceInProgress bool
}

// SetCheckout records what the cwd syncer read of an agent's checkout.
func (s *AgentStore) SetCheckout(ctx context.Context, id string, params SetCheckoutParams) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	err := s.queries.SetAgentCheckoutByID(ctx, models.SetAgentCheckoutByIDParams{
		ID:                 id,
		Branch:             params.Branch,
		RepositoryUrl:      params.RepositoryURL,
		WorktreeRemoved:    params.WorktreeRemoved,
		SequenceInProgress: params.SequenceInProgress,
	})
	if err != nil {
		return fmt.Errorf("failed to set the checkout for id %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return nil
}

// SetSequenceInProgressByID records whether a merge, a rebase or a cherry-pick is part-way
// through an agent's checkout.
func (s *AgentStore) SetSequenceInProgressByID(ctx context.Context, id string, inProgress bool) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	if err := s.queries.SetAgentSequenceInProgressByID(ctx, models.SetAgentSequenceInProgressByIDParams{ID: id, SequenceInProgress: inProgress}); err != nil {
		return fmt.Errorf("failed to set sequence_in_progress for id %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return nil
}

// SetInterruptedAtByID records when an agent's conversation was cut off part-way through a
// turn.
func (s *AgentStore) SetInterruptedAtByID(ctx context.Context, id string, at *time.Time) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentInterruptedAtByID(ctx, models.SetAgentInterruptedAtByIDParams{ID: id, InterruptedAt: at}); err != nil {
		return fmt.Errorf("failed to set interrupted_at for id %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return nil
}

// Hold records that a person has stopped an agent, and reports false when somebody already had it.
func (s *AgentStore) Hold(ctx context.Context, params HoldParams) (bool, error) {
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

	s.db.Touch(store.TableAgents, params.ID)

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
func (s *AgentStore) Release(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	released, err := s.queries.ReleaseAgentByID(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to release agent %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return len(released) > 0, nil
}

type SetHarnessParams struct {
	ID        string
	Harness   string
	Flags     string
	SessionID string
}

// SetHarness moves an agent onto another CLI, with the flags and the conversation that belong
// to it.
func (s *AgentStore) SetHarness(ctx context.Context, params SetHarnessParams) error {
	if !store.IsID(params.ID) {
		return store.ErrNotFound.Saying("no agent with id %q", params.ID)
	}

	if err := s.queries.SetAgentHarness(ctx, models.SetAgentHarnessParams{
		ID:        params.ID,
		Harness:   params.Harness,
		Flags:     params.Flags,
		SessionID: params.SessionID,
	}); err != nil {
		return fmt.Errorf("failed to set the harness for id %q: %w", params.ID, err)
	}

	s.db.Touch(store.TableAgents, params.ID)

	return nil
}

// SetSessionID records an agent's conversation when none is already stored.
func (s *AgentStore) SetSessionID(ctx context.Context, id, sessionID string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentSessionID(ctx, models.SetAgentSessionIDParams{ID: id, SessionID: sessionID}); err != nil {
		return fmt.Errorf("failed to set the session for id %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

	return nil
}

// SetAlive records whether each of the given agents still owns its pane.
func (s *AgentStore) SetAlive(ctx context.Context, ids []string, alive bool) error {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.SetAgentsAlive(ctx, models.SetAgentsAliveParams{Ids: storable, Alive: alive}); err != nil {
		return fmt.Errorf("failed to set %d agents alive=%t: %w", len(storable), alive, err)
	}

	s.db.Touch(store.TableAgents, storable...)

	return nil
}

// SetTreeStanding records where an agent's worktree stands against the round it was measured
// against.
func (s *AgentStore) SetTreeStanding(ctx context.Context, id string, standing git.Standing, snapshotSHA string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if err := s.queries.SetAgentTreeStanding(ctx, models.SetAgentTreeStandingParams{
		ID:              id,
		TreeStanding:    string(standing),
		TreeStandingSha: snapshotSHA,
	}); err != nil {
		return fmt.Errorf("failed to set the tree standing for id %q: %w", id, err)
	}

	s.db.Touch(store.TableAgents, id)

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
func (s *AgentStore) ForgetPane(ctx context.Context, params ForgetPaneParams) (bool, error) {
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

	s.db.Touch(store.TableAgents, params.ID)

	return len(forgotten) > 0, nil
}
