package agent

import (
	"context"
	"fmt"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

// List returns every persisted agent, newest first.
func (s *Store) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// Cursor pages this listing, which resumes from one row's id.
type Cursor = store.IDCursor

// AgentDir is the directory one agent holds, which is all a check on the worktrees weave
// made needs to know about it.
type AgentDir struct {
	ID  string
	Cwd string
}

// ListDirs returns the directory every agent holds, archived ones included since a dormant
// agent is restored into the tree it still owns.
func (s *Store) ListDirs(ctx context.Context) ([]AgentDir, error) {
	rows, err := s.queries.ListAgentDirs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the directories agents hold: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListAgentDirsRow) AgentDir {
		return AgentDir{ID: row.ID, Cwd: row.Cwd}
	}), nil
}

// ListPage returns one page of live or archived agents, newest first.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor, archived bool) ([]Agent, error) {
	params := models.ListAgentsPageParams{Lim: int32(limit), Archived: archived}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListAgentsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of agents: %w", err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// ListAll returns every agent including archived ones, newest first, for search.
func (s *Store) ListAll(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAllAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all agents: %w", err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// GetInWorkflow returns a live agent by workflow and name.
func (s *Store) GetInWorkflow(ctx context.Context, workflowID, name string) (*Agent, error) {
	row, err := s.queries.GetAgentInWorkflow(ctx, models.GetAgentInWorkflowParams{
		WorkflowID: workflowID,
		Name:       name,
	})
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: agent %q in workflow %q", store.ErrNotFound, name, workflowID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q in workflow %q: %w", name, workflowID, err)
	}

	return s.hydrateMetadataForAgent(ctx, row)
}

// GetByID returns a live or archived agent by ID.
func (s *Store) GetByID(ctx context.Context, id string) (*Agent, error) {
	if !store.IsID(id) {
		return nil, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}

	row, err := s.queries.GetAgentByID(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", id, err)
	}

	return s.hydrateMetadataForAgent(ctx, row)
}

// ListByID returns live and archived agents matching the given IDs.
func (s *Store) ListByID(ctx context.Context, ids []string) ([]Agent, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.GetAgentsByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d agents: %w", len(storable), err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// Exists reports whether any agent holds the given id, including archived ones.
func (s *Store) Exists(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	exists, err := s.queries.AgentExists(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to check for agent %q: %w", id, err)
	}

	return exists, nil
}

// ActiveNameExists reports whether a live agent holds the name in a workflow.
func (s *Store) ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error) {
	exists, err := s.queries.ActiveNameExists(ctx, models.ActiveNameExistsParams{
		WorkflowID: workflowID,
		Name:       name,
	})
	if err != nil {
		return false, fmt.Errorf("failed to check active name %q in workflow %q: %w", name, workflowID, err)
	}

	return exists, nil
}

// NamesHoldingSchedule returns the names of the agents outside a workflow that still point
// at a schedule, which is only the ones a schedule moved between workflows left behind.
func (s *Store) NamesHoldingSchedule(ctx context.Context, scheduleID, workflowID string) ([]string, error) {
	if !store.IsID(scheduleID) || !store.IsID(workflowID) {
		return nil, nil
	}

	names, err := s.queries.ListScheduleAgentNamesOutsideWorkflow(ctx, models.ListScheduleAgentNamesOutsideWorkflowParams{
		ScheduleID: &scheduleID,
		WorkflowID: workflowID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents of schedule %q: %w", scheduleID, err)
	}

	return names, nil
}

// ListLiveInWorkflow returns the live agents in a workflow.
func (s *Store) ListLiveInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListLiveWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list live agents of workflow %q: %w", workflowID, err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// ListArchivedInWorkflow returns the archived agents in a workflow.
func (s *Store) ListArchivedInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListArchivedWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list archived agents of workflow %q: %w", workflowID, err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// ListChildren returns the agents attached to the given parents, archived ones included, so a
// caller that has to include a family decides for itself which of them it wants.
func (s *Store) ListChildren(ctx context.Context, parentIDs []string) ([]Agent, error) {
	storable := keepStorableIDs(parentIDs)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListChildAgents(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the children of %d agents: %w", len(storable), err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// CountLiveChildren returns how many live agents are attached to one parent.
func (s *Store) CountLiveChildren(ctx context.Context, parentID string) (int, error) {
	if !store.IsID(parentID) {
		return 0, nil
	}

	count, err := s.queries.CountLiveChildAgents(ctx, &parentID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the children of agent %q: %w", parentID, err)
	}

	return int(count), nil
}
