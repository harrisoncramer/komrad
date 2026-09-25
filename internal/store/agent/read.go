package agent

import (
	"context"
	"fmt"
	"time"

	"komrade/db"
	"komrade/db/models"
	"komrade/internal/store"
	"komrade/lib/xslice"
)

// List returns every persisted agent, newest first.
func (s *AgentStore) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// Cursor pages this listing, which resumes from one row's id.
type Cursor = store.IDCursor

// AgentDir is the directory one agent holds, with what a listing of directories says about
// the agent.
type AgentDir struct {
	ID             string
	Name           string
	Cwd            string
	State          State
	StateChangedAt time.Time
	ArchivedAt     *time.Time
	DeletingAt     *time.Time
	AdoptedBranch  string
	WorkflowID     string
}

// ListDirs returns the directory every agent holds, archived ones included.
func (s *AgentStore) ListDirs(ctx context.Context) ([]AgentDir, error) {
	rows, err := s.queries.ListAgentDirs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the directories agents hold: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListAgentDirsRow) AgentDir {
		return AgentDir{
			ID:             row.ID,
			Name:           row.Name,
			Cwd:            row.Cwd,
			State:          State(row.State),
			StateChangedAt: row.StateChangedAt,
			ArchivedAt:     row.ArchivedAt,
			DeletingAt:     row.DeletingAt,
			AdoptedBranch:  row.AdoptedBranch,
			WorkflowID:     row.WorkflowID,
		}
	}), nil
}

// ListArchiveSweepPage returns one page of archived agents, newest first, including the rows a
// delete has already claimed.
func (s *AgentStore) ListArchiveSweepPage(ctx context.Context, limit int64, cursor *Cursor) ([]Agent, error) {
	params := models.ListArchiveSweepPageParams{Lim: int32(limit)}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListArchiveSweepPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of the archive for the sweep: %w", err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// ListAll returns every agent including archived ones, newest first, for search.
func (s *AgentStore) ListAll(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAllAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all agents: %w", err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// ListForTranscriptSync returns every agent whose log can still hold turns nobody has read: the
// live ones, and the archived ones holding a conversation last synced before they were archived.
func (s *AgentStore) ListForTranscriptSync(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAgentsForTranscriptSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for transcript sync: %w", err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// GetInWorkflow returns a live agent by workflow and name.
func (s *AgentStore) GetInWorkflow(ctx context.Context, workflowID, name string) (*Agent, error) {
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

// ListLiveNamed returns the live agents holding a name, across every workflow.
func (s *AgentStore) ListLiveNamed(ctx context.Context, name string) ([]Agent, error) {
	rows, err := s.queries.ListLiveAgentsNamed(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to list the live agents named %q: %w", name, err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// GetByID returns a live or archived agent by ID.
func (s *AgentStore) GetByID(ctx context.Context, id string) (*Agent, error) {
	if !store.IsID(id) {
		return nil, store.ErrNotFound.Saying("no agent with id %q", id)
	}

	row, err := s.queries.GetAgentByID(ctx, id)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no agent with id %q", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", id, err)
	}

	return s.hydrateMetadataForAgent(ctx, row)
}

// ListByID returns live and archived agents matching the given IDs.
func (s *AgentStore) ListByID(ctx context.Context, ids []string) ([]Agent, error) {
	storable := store.KeepIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListAgentsByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d agents: %w", len(storable), err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// Exists reports whether any agent holds the given id, including archived ones.
func (s *AgentStore) Exists(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	exists, err := s.queries.AgentExists(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to check for agent %q: %w", id, err)
	}

	return exists, nil
}

// ExistsActiveName reports whether a live agent holds the name in a workflow.
func (s *AgentStore) ExistsActiveName(ctx context.Context, workflowID, name string) (bool, error) {
	exists, err := s.queries.ActiveNameExists(ctx, models.ActiveNameExistsParams{
		WorkflowID: workflowID,
		Name:       name,
	})
	if err != nil {
		return false, fmt.Errorf("failed to check active name %q in workflow %q: %w", name, workflowID, err)
	}

	return exists, nil
}

// ListNamesHoldingSchedule returns the names of the agents outside a workflow that still point
// at a schedule.
func (s *AgentStore) ListNamesHoldingSchedule(ctx context.Context, scheduleID, workflowID string) ([]string, error) {
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
func (s *AgentStore) ListLiveInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListLiveWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list live agents of workflow %q: %w", workflowID, err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// ListArchivedInWorkflow returns the archived agents in a workflow.
func (s *AgentStore) ListArchivedInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListArchivedWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list archived agents of workflow %q: %w", workflowID, err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// ListChildren returns the agents attached to the given parents, archived ones included.
func (s *AgentStore) ListChildren(ctx context.Context, parentIDs []string) ([]Agent, error) {
	storable := store.KeepIDs(parentIDs)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListChildAgents(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the children of %d agents: %w", len(storable), err)
	}

	return s.hydrateMetadataForAgents(ctx, rows)
}

// CountLiveSpawned returns how many live agents one agent spawned that are not attached to a parent.
func (s *AgentStore) CountLiveSpawned(ctx context.Context, spawnerID string) (int, error) {
	if !store.IsID(spawnerID) {
		return 0, nil
	}

	count, err := s.queries.CountLiveSpawnedAgents(ctx, &spawnerID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the agents spawned by %q: %w", spawnerID, err)
	}

	return int(count), nil
}

// CountLiveChildren returns how many live agents are attached to one parent.
func (s *AgentStore) CountLiveChildren(ctx context.Context, parentID string) (int, error) {
	if !store.IsID(parentID) {
		return 0, nil
	}

	count, err := s.queries.CountLiveChildAgents(ctx, &parentID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the children of agent %q: %w", parentID, err)
	}

	return int(count), nil
}

// GetWorkflowOrchestrator returns the orchestrator a workflow holds, live or archived, and
// ErrNotFound when it has none.
func (s *AgentStore) GetWorkflowOrchestrator(ctx context.Context, workflowID string) (*Agent, error) {
	if !store.IsID(workflowID) {
		return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, workflowID)
	}

	id, err := s.queries.GetWorkflowOrchestratorID(ctx, workflowID)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: workflow %q has no orchestrator", store.ErrNotFound, workflowID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the orchestrator of workflow %q: %w", workflowID, err)
	}

	return s.GetByID(ctx, id)
}
