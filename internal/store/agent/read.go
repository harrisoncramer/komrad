package agent

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
)

// List returns every persisted agent, newest first.
func (s *AgentStore) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return fromRows(rows), nil
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
	WorkflowID     string
	CreatedAt      time.Time
	// ProvisionedTaskID is the task the agent was provisioned a worktree for, empty for an agent other than a writer or one that keeps the directory it was given.
	ProvisionedTaskID string
}

// ListDirs returns the directory every agent holds, archived ones included.
func (s *AgentStore) ListDirs(ctx context.Context) ([]AgentDir, error) {
	rows, err := s.queries.ListAgentDirs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the directories agents hold: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListAgentDirsRow) AgentDir {
		return AgentDir{
			ID:                row.ID,
			Name:              row.Name,
			Cwd:               row.Cwd,
			State:             State(row.State),
			StateChangedAt:    row.StateChangedAt,
			ArchivedAt:        row.ArchivedAt,
			DeletingAt:        row.DeletingAt,
			WorkflowID:        row.WorkflowID,
			CreatedAt:         row.CreatedAt,
			ProvisionedTaskID: provisionedTaskIDOf(row),
		}
	}), nil
}

// ListAll returns every agent including archived ones, newest first, for search.
func (s *AgentStore) ListAll(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAllAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all agents: %w", err)
	}

	return fromRows(rows), nil
}

// ListForTranscriptSync returns every agent whose log can still hold turns nobody has read: the
// live ones, and the archived ones holding a conversation last synced before they were archived.
func (s *AgentStore) ListForTranscriptSync(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAgentsForTranscriptSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents for transcript sync: %w", err)
	}

	return fromRows(rows), nil
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

	return xptr.Ptr(FromRow(*row)), nil
}

// ListLiveNamed returns the live agents holding a name, across every workflow.
func (s *AgentStore) ListLiveNamed(ctx context.Context, name string) ([]Agent, error) {
	rows, err := s.queries.ListLiveAgentsNamed(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to list the live agents named %q: %w", name, err)
	}

	return fromRows(rows), nil
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

	return xptr.Ptr(FromRow(*row)), nil
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

	return fromRows(rows), nil
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

// ListLiveInWorkflow returns the live agents in a workflow.
func (s *AgentStore) ListLiveInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListLiveWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list live agents of workflow %q: %w", workflowID, err)
	}

	return fromRows(rows), nil
}

// ListLiveOnTask returns the live agents working on a task.
func (s *AgentStore) ListLiveOnTask(ctx context.Context, taskID string) ([]Agent, error) {
	rows, err := s.queries.ListLiveTaskAgents(ctx, &taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to list live agents of task %q: %w", taskID, err)
	}

	return fromRows(rows), nil
}

// ListOnTasks returns every agent on the tasks among taskIDs, archived ones included.
func (s *AgentStore) ListOnTasks(ctx context.Context, taskIDs []string) ([]Agent, error) {
	storable := store.KeepIDs(taskIDs)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListAgentsOnTasks(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents of %d tasks: %w", len(storable), err)
	}

	return fromRows(rows), nil
}

// GetTaskWriter returns the live writer of a task, ErrNotFound when it has none.
func (s *AgentStore) GetTaskWriter(ctx context.Context, taskID string) (*Agent, error) {
	if !store.IsID(taskID) {
		return nil, store.ErrNotFound.Saying("no writer on task %q", taskID)
	}

	row, err := s.queries.GetTaskWriter(ctx, taskID)
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no writer on task %q", taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the writer of task %q: %w", taskID, err)
	}

	return xptr.Ptr(FromRow(*row)), nil
}

// ListArchivedInWorkflow returns the archived agents in a workflow.
func (s *AgentStore) ListArchivedInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListArchivedWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list archived agents of workflow %q: %w", workflowID, err)
	}

	return fromRows(rows), nil
}

// ListRestorableInWorkflow returns the archived agents a workflow's restore brings back, leaving
// reviewers archived.
func (s *AgentStore) ListRestorableInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListRestorableWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the restorable agents of workflow %q: %w", workflowID, err)
	}

	return fromRows(rows), nil
}

// CountLiveTasksStartedBy returns how many live tasks one agent started, counted by their writers.
func (s *AgentStore) CountLiveTasksStartedBy(ctx context.Context, spawnerID string) (int, error) {
	if !store.IsID(spawnerID) {
		return 0, nil
	}

	count, err := s.queries.CountLiveTasksStartedBy(ctx, &spawnerID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the tasks started by %q: %w", spawnerID, err)
	}

	return int(count), nil
}

// CountLiveReviewers returns how many live reviewers one task holds.
func (s *AgentStore) CountLiveReviewers(ctx context.Context, taskID string) (int, error) {
	if !store.IsID(taskID) {
		return 0, nil
	}

	count, err := s.queries.CountLiveTaskReviewers(ctx, &taskID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the reviewers of task %q: %w", taskID, err)
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

func provisionedTaskIDOf(row *models.ListAgentDirsRow) string {
	if Role(row.Role) != RoleWriter || IsProvisionerDisabled(row.ProvisionEnabled) {
		return ""
	}

	return xptr.Deref(row.TaskID)
}
