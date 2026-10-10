// Package tasks owns the rules for tasks, the outcomes agents are linked to and work toward.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/internal/fail"
	"komrad/internal/store"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xstr"
)

const maxSlugBase = 60

var (
	// ErrOtherWorkflow is a task filed under a different workflow from the agent joining it.
	ErrOtherWorkflow = fail.InvalidArgument("an agent works only on a task in its own workflow")
	// errArchived is an archived task an agent was asked to join.
	errArchived = fail.Precondition("an agent works only on a live task")
)

type NewTaskServiceParams struct {
	Tasks *taskstore.TaskStore
}

type TaskService struct {
	tasks *taskstore.TaskStore
}

// NewTaskService builds the service from its dependencies.
func NewTaskService(params NewTaskServiceParams) *TaskService {
	return &TaskService{tasks: params.Tasks}
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

// UpdateTask rewrites the fields params sets on the task an id or a slug names.
func (s *TaskService) UpdateTask(ctx context.Context, ref string, params taskstore.UpdateParams) (*taskstore.Task, error) {
	found, err := s.GetTask(ctx, ref)
	if err != nil {
		return nil, err
	}

	return s.tasks.Update(ctx, found.ID, params)
}

// ArchiveTask archives the task an id or a slug names, leaving one already archived as it was.
func (s *TaskService) ArchiveTask(ctx context.Context, ref string) error {
	found, err := s.GetTask(ctx, ref)
	if err != nil {
		return err
	}

	return s.tasks.Archive(ctx, found.ID)
}

// GetTaskForWorkflow returns the live task an id or a slug names in a workflow.
func (s *TaskService) GetTaskForWorkflow(ctx context.Context, ref, workflowID string) (*taskstore.Task, error) {
	found, err := s.GetTask(ctx, ref)
	if err != nil {
		return nil, err
	}
	if found.WorkflowID != workflowID {
		return nil, ErrOtherWorkflow.Saying("task %q is in workflow %q, not %q", found.Slug, found.WorkflowID, workflowID)
	}
	if found.ArchivedAt != nil {
		return nil, errArchived.Saying("task %q is archived", found.Slug)
	}

	return found, nil
}

// SetCheckoutDir records the directory a launch provisioned for a task.
func (s *TaskService) SetCheckoutDir(ctx context.Context, id, cwd string) error {
	return s.tasks.SetCheckoutDir(ctx, id, cwd)
}

// ClaimCheckoutAgain claims a task's checkout again for a retried launch, reporting false when the
// task already has one.
func (s *TaskService) ClaimCheckoutAgain(ctx context.Context, id string) (bool, error) {
	return s.tasks.ClaimCheckoutAgain(ctx, id)
}

// ReleaseCheckout gives up the claim a failed launch made on a task's checkout.
func (s *TaskService) ReleaseCheckout(ctx context.Context, id string) error {
	return s.tasks.ReleaseCheckout(ctx, id)
}

// ListTasksByID returns the tasks among ids, archived or not.
func (s *TaskService) ListTasksByID(ctx context.Context, ids []string) ([]taskstore.Task, error) {
	return s.tasks.ListByID(ctx, ids)
}

type CreateForAgentParams struct {
	WorkflowID string
	Name       string
	Title      string
	Prompt     string
	Slug       string
	Goal       string
}

// CreateForAgent creates a task for an agent, under the slug given or one named after the agent.
func CreateForAgent(ctx context.Context, tasks *taskstore.TaskStore, params CreateForAgentParams) (*taskstore.Task, error) {
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
func pickSlug(ctx context.Context, tasks *taskstore.TaskStore, params CreateForAgentParams) (string, error) {
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
