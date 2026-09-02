package agent

import (
	"context"
	"fmt"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

// Create inserts an agent and assigns an ID when needed.
func (s *Store) Create(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		id, err := store.NewID()
		if err != nil {
			return err
		}

		agent.ID = id
	}

	if _, err := s.queries.InsertAgent(ctx, models.InsertAgentParams{
		ID:               agent.ID,
		Name:             agent.Name,
		Cwd:              agent.Cwd,
		Flags:            agent.Flags,
		Prompt:           agent.Prompt,
		PaneID:           agent.Pane,
		State:            agent.State,
		Harness:          agent.Harness,
		SessionID:        agent.SessionID,
		WorkflowID:       agent.WorkflowID,
		ScheduleID:       agent.ScheduleID,
		IdempotencyKey:   agent.IdempotencyKey,
		IsHidden:         agent.Hidden,
		SetupAction:      agent.SetupAction,
		CleanupAction:    agent.CleanupAction,
		SpawnedByAgentID: agent.SpawnedByAgentID,
		ParentAgentID:    agent.ParentAgentID,
		AdoptedBranch:    agent.AdoptedBranch,
	}); err != nil {
		return fmt.Errorf("failed to insert agent %q: %w", agent.Name, err)
	}

	return nil
}

// Update writes an agent's editable and runtime fields.
func (s *Store) Update(ctx context.Context, agent Agent) error {
	if err := s.queries.UpdateAgent(ctx, models.UpdateAgentParams{
		ID:         agent.ID,
		Cwd:        agent.Cwd,
		Flags:      agent.Flags,
		Prompt:     agent.Prompt,
		PaneID:     agent.Pane,
		State:      agent.State,
		SessionID:  agent.SessionID,
		WorkflowID: agent.WorkflowID,
	}); err != nil {
		return fmt.Errorf("failed to update agent %q: %w", agent.Name, err)
	}

	return nil
}

// UpdateIfLive rewrites an agent's row and reports whether it did, refusing one that an
// archive or a delete took while a resume was mid-flight.
func (s *Store) UpdateIfLive(ctx context.Context, agent Agent) (bool, error) {
	if !store.IsID(agent.ID) {
		return false, nil
	}

	updated, err := s.queries.UpdateAgentIfLive(ctx, models.UpdateAgentIfLiveParams{
		ID:         agent.ID,
		Cwd:        agent.Cwd,
		Flags:      agent.Flags,
		Prompt:     agent.Prompt,
		PaneID:     agent.Pane,
		State:      agent.State,
		SessionID:  agent.SessionID,
		WorkflowID: agent.WorkflowID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to update agent %q: %w", agent.Name, err)
	}

	return len(updated) > 0, nil
}

// Delete removes live or archived agents by ID.
func (s *Store) Delete(ctx context.Context, ids []string) ([]AgentPane, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	deleted, err := s.queries.DeleteAgentsByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to delete %d agents: %w", len(storable), err)
	}

	return xslice.Map(deleted, func(row *models.DeleteAgentsByIDRow) AgentPane {
		return AgentPane{AgentID: row.ID, Pane: row.PaneID}
	}), nil
}

// Rename changes an agent's display name and reports whether a row changed.
func (s *Store) Rename(ctx context.Context, id, name string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	renamed, err := s.queries.RenameAgent(ctx, models.RenameAgentParams{ID: id, Name: name})
	if db.IsUniqueViolation(err) {
		return false, store.ErrNameTaken
	}
	if err != nil {
		return false, fmt.Errorf("failed to rename agent %q: %w", id, err)
	}

	return len(renamed) > 0, nil
}
