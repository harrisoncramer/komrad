package agent

import (
	"context"
	"fmt"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// Create inserts an agent and assigns an ID when needed.
func (s *AgentStore) Create(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		id, err := store.NewID()
		if err != nil {
			return err
		}

		agent.ID = id
	}

	if _, err := s.queries.InsertAgent(ctx, models.InsertAgentParams{
		ID:                 agent.ID,
		Name:               agent.Name,
		Cwd:                agent.Cwd,
		Flags:              agent.Flags,
		Prompt:             agent.Prompt,
		PaneID:             agent.Pane,
		Role:               models.AgentRole(agent.Role.orDefault()),
		State:              models.AgentState(agent.State),
		Harness:            agent.Harness,
		SessionID:          agent.SessionID,
		WorkflowID:         agent.WorkflowID,
		ScheduleID:         agent.ScheduleID,
		IdempotencyKey:     agent.IdempotencyKey,
		IsHidden:           agent.Hidden,
		ProvisionEnabled:   agent.ProvisionEnabled,
		SpawnedByAgentID:   agent.SpawnedByAgentID,
		RequestedByAgentID: agent.RequestedByAgentID,
		ParentAgentID:      agent.ParentAgentID,
		AdoptedBranch:      agent.AdoptedBranch,
		Branch:             agent.Branch,
		RepositoryUrl:      agent.RepositoryURL,
		TaskID:             agent.TaskID,
	}); err != nil {
		return fmt.Errorf("failed to insert agent %q: %w", agent.Name, err)
	}

	return nil
}

// Update writes an agent's editable and runtime fields.
func (s *AgentStore) Update(ctx context.Context, agent Agent) error {
	if err := s.queries.UpdateAgent(ctx, models.UpdateAgentParams{
		ID:         agent.ID,
		Cwd:        agent.Cwd,
		Flags:      agent.Flags,
		Prompt:     agent.Prompt,
		PaneID:     agent.Pane,
		State:      models.AgentState(agent.State),
		SessionID:  agent.SessionID,
		WorkflowID: agent.WorkflowID,
	}); err != nil {
		return fmt.Errorf("failed to update agent %q: %w", agent.Name, err)
	}

	return nil
}

// UpdateIfLive rewrites an agent's row and reports whether it did, refusing an archived or
// deleted one.
func (s *AgentStore) UpdateIfLive(ctx context.Context, agent Agent) (bool, error) {
	if !store.IsID(agent.ID) {
		return false, nil
	}

	updated, err := s.queries.UpdateAgentIfLive(ctx, models.UpdateAgentIfLiveParams{
		ID:         agent.ID,
		Cwd:        agent.Cwd,
		Flags:      agent.Flags,
		Prompt:     agent.Prompt,
		PaneID:     agent.Pane,
		State:      models.AgentState(agent.State),
		SessionID:  agent.SessionID,
		WorkflowID: agent.WorkflowID,
	})
	if err != nil {
		return false, fmt.Errorf("failed to update agent %q: %w", agent.Name, err)
	}

	return len(updated) > 0, nil
}

// Delete removes live or archived agents by ID.
func (s *AgentStore) Delete(ctx context.Context, ids []string) ([]AgentPane, error) {
	storable := store.KeepIDs(ids)
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
func (s *AgentStore) Rename(ctx context.Context, id, name string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	renamed, err := s.queries.RenameAgent(ctx, models.RenameAgentParams{ID: id, Name: name})
	if db.IsUniqueViolation(err) {
		return false, store.ErrNameTaken.Saying("an agent named %q already exists", name)
	}
	if err != nil {
		return false, fmt.Errorf("failed to rename agent %q: %w", id, err)
	}

	return len(renamed) > 0, nil
}

// SetTaskID links an agent to a task, or unlinks it when taskID is nil.
func (s *AgentStore) SetTaskID(ctx context.Context, id string, taskID *string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	if taskID != nil && !store.IsID(*taskID) {
		return store.ErrNotFound.Saying("no task with id %q", *taskID)
	}

	updated, err := s.queries.SetAgentTaskID(ctx, models.SetAgentTaskIDParams{ID: id, TaskID: taskID})
	if db.IsForeignKeyViolation(err, "agents_task_id_fkey") {
		return store.ErrNotFound.Saying("no task with id %q", *taskID)
	}
	if err != nil {
		return fmt.Errorf("failed to set the task of agent %q: %w", id, err)
	}

	if updated == 0 {
		return store.ErrNotFound.Saying("no agent with id %q", id)
	}

	return nil
}
