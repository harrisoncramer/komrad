// Package tasks owns the rules for tasks, the outcomes agents are linked to and work toward.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/scriptrun"
	"komrad/internal/store"
	"komrad/lib/xstr"

	agentstore "komrad/internal/store/agent"
	runstore "komrad/internal/store/run"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
)

const maxSlugBase = 60

var (
	// ErrOtherWorkflow is a task filed under a different workflow from the agent joining it.
	ErrOtherWorkflow = fail.InvalidArgument("an agent works only on a task in its own workflow")
	// ErrNotesNotYours is an agent writing the notes of a task it does not work on or orchestrate.
	ErrNotesNotYours = fail.Denied("an agent writes only the notes of its own task or of one in the workflow it orchestrates")
	// errHasCheckout is a launch that would make a checkout for a task that already has one.
	errHasCheckout = fail.Precondition("this task already has a checkout, so launch a child of the agent working in it or give this one a task of its own")
	// ErrHasWriter is a writer launched onto a task whose writer is still live.
	ErrHasWriter = fail.Precondition("this task already has a live writer, so launch a child of it or hand the task off")
	// ErrWorktreeRemoved is a writer launched onto a task whose worktree was removed.
	ErrWorktreeRemoved = fail.Precondition("this task's worktree was removed, so give this agent a task of its own")
	// ErrArchived is an archived task an agent was asked to join.
	ErrArchived = fail.Precondition("an agent works only on a live task")
)

type txRunner interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

// taskScriptRunner runs a task's hooks and gives back the checkout of a task no agent is left on.
type taskScriptRunner interface {
	scriptrun.HookRunner
	RunTaskDeprovision(ctx context.Context, task taskstore.Task, name string) error
}

type NewTaskServiceParams struct {
	Tasks     *taskstore.TaskStore
	Agents    *agentstore.AgentStore
	Runs      *runstore.RunStore
	Workflows *workflowstore.WorkflowStore
	Worktrees *worktreestore.WorktreeStore
	Scripts   taskScriptRunner
	Tx        txRunner
}

type TaskService struct {
	tasks     *taskstore.TaskStore
	agents    *agentstore.AgentStore
	runs      *runstore.RunStore
	workflows *workflowstore.WorkflowStore
	worktrees *worktreestore.WorktreeStore
	scripts   taskScriptRunner
	tx        txRunner
}

// NewTaskService builds the service from its dependencies.
func NewTaskService(params NewTaskServiceParams) *TaskService {
	return &TaskService{
		tasks:     params.Tasks,
		agents:    params.Agents,
		runs:      params.Runs,
		workflows: params.Workflows,
		worktrees: params.Worktrees,
		scripts:   params.Scripts,
		tx:        params.Tx,
	}
}

// CreateTask writes a new task, refusing a slug that is malformed or already used.
func (s *TaskService) CreateTask(ctx context.Context, params taskstore.CreateParams) (*taskstore.Task, error) {
	return s.tasks.Create(ctx, params)
}

// GetTask returns the task an id or a slug names, archived or not.
func (s *TaskService) GetTask(ctx context.Context, ref string) (*taskstore.Task, error) {
	if store.IsID(ref) {
		found, err := s.tasks.Get(ctx, ref)
		if !errors.Is(err, store.ErrNotFound) {
			return found, err
		}
	}

	return s.tasks.GetBySlug(ctx, ref)
}

// ListTasks returns a workflow's live or archived tasks, the newest first.
func (s *TaskService) ListTasks(ctx context.Context, workflowID string, archived bool) ([]taskstore.Task, error) {
	return s.tasks.List(ctx, workflowID, archived)
}

// ListRuns returns every script run recorded on a task, by any agent on it, newest first.
func (s *TaskService) ListRuns(ctx context.Context, taskID string) ([]runstore.Run, error) {
	return s.runs.ListByTask(ctx, taskID)
}

// MarkTaskRunsRead claims the failed runs on a task.
func (s *TaskService) MarkTaskRunsRead(ctx context.Context, taskID string) error {
	return s.runs.MarkTaskRead(ctx, taskID)
}

type UpdateTaskParams struct {
	Ref           string
	Fields        taskstore.UpdateParams
	CallerAgentID string
}

// UpdateTask rewrites the fields params sets on the task an id or a slug names, refusing notes
// from a caller that may not write them.
func (s *TaskService) UpdateTask(ctx context.Context, params UpdateTaskParams) (*taskstore.Task, error) {
	found, err := s.GetTask(ctx, params.Ref)
	if err != nil {
		return nil, err
	}
	if params.Fields.Notes != nil {
		if err := s.requireMayWriteNotes(ctx, params.CallerAgentID, *found); err != nil {
			return nil, err
		}
	}

	return s.tasks.Update(ctx, found.ID, params.Fields)
}

// requireMayWriteNotes refuses a calling agent that neither works on the task nor orchestrates its workflow.
func (s *TaskService) requireMayWriteNotes(ctx context.Context, callerAgentID string, target taskstore.Task) error {
	if callerAgentID == "" {
		return nil
	}

	caller, err := s.agents.GetByID(ctx, callerAgentID)
	if err != nil {
		return err
	}
	if !mayWriteNotes(*caller, target) {
		return fmt.Errorf("%w: %q on task %q", ErrNotesNotYours, caller.Name, target.Slug)
	}

	return nil
}

// mayWriteNotes reports whether an agent may write a task's notes, being on the task or the
// orchestrator of its workflow.
func mayWriteNotes(caller agentstore.Agent, target taskstore.Task) bool {
	if caller.Role == agentstore.RoleOrchestrator {
		return caller.WorkflowID == target.WorkflowID
	}

	return caller.TaskID != nil && *caller.TaskID == target.ID
}

// Archive archives a task, leaving one already archived as it was.
func (s *TaskService) Archive(ctx context.Context, id string) error {
	return s.tasks.Archive(ctx, id)
}

// GetTaskForWorkflow returns the task an id or a slug names in a workflow, archived or not.
func (s *TaskService) GetTaskForWorkflow(ctx context.Context, ref, workflowID string) (*taskstore.Task, error) {
	found, err := s.GetTask(ctx, ref)
	if err != nil {
		return nil, err
	}
	if found.WorkflowID != workflowID {
		return nil, ErrOtherWorkflow.Saying("task %q is in workflow %q, not %q", found.Slug, found.WorkflowID, workflowID)
	}

	return found, nil
}

// SetCheckoutDir records the directory a launch provisioned for a task.
func (s *TaskService) SetCheckoutDir(ctx context.Context, id, cwd string) error {
	return s.tasks.SetCheckoutDir(ctx, id, cwd)
}

// ReleaseCheckout gives up the claim a failed launch made on a task's checkout.
func (s *TaskService) ReleaseCheckout(ctx context.Context, id string) error {
	return s.tasks.ReleaseCheckout(ctx, id)
}

// ListTasksByID returns the tasks among ids, archived or not.
func (s *TaskService) ListTasksByID(ctx context.Context, ids []string) ([]taskstore.Task, error) {
	return s.tasks.ListByID(ctx, ids)
}

type createForAgentParams struct {
	WorkflowID string
	Name       string
	Title      string
	Prompt     string
	Slug       string
	Goal       string
}

// createForAgent creates a task for an agent, under the slug given or one named after the agent.
func createForAgent(ctx context.Context, tasks *taskstore.TaskStore, params createForAgentParams) (*taskstore.Task, error) {
	slug, err := pickSlug(ctx, tasks, params)
	if err != nil {
		return nil, err
	}

	title := strings.TrimSpace(params.Title)
	if title == "" {
		title = params.Name
	}

	goal := strings.TrimSpace(params.Goal)
	if goal == "" {
		goal = strings.TrimSpace(xstr.FirstLine(strings.TrimSpace(params.Prompt)))
	}

	return tasks.Create(ctx, taskstore.CreateParams{
		WorkflowID: params.WorkflowID,
		Slug:       slug,
		Title:      title,
		Goal:       goal,
	})
}

// pickSlug is the slug asked for, or the first numbered from the agent's name that no task has had.
func pickSlug(ctx context.Context, tasks *taskstore.TaskStore, params createForAgentParams) (string, error) {
	if params.Slug != "" {
		return params.Slug, nil
	}
	if err := tasks.LockSlugs(ctx); err != nil {
		return "", err
	}

	base := xstr.Slug(params.Name, maxSlugBase)
	if base == "" {
		base = "task"
	}

	slug := base
	// Tries fix-login, then fix-login-2 and on, the bare slug counting as the first.
	for i := 2; ; i++ {
		taken, err := tasks.IsSlugTaken(ctx, slug)
		if err != nil {
			return "", err
		}
		if !taken {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}

	return slug, nil
}
