// Package task stores tasks, the outcomes agents are linked to and work toward.
package task

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// ErrBadSlug is a slug that is not lowercase kebab-case.
var ErrBadSlug = fail.InvalidArgument("a task slug is lowercase letters and digits joined by single hyphens")

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Task is one outcome a person wants, which the agents linked to it work toward.
type Task struct {
	ID         string
	WorkflowID string
	Slug       string
	Title      string
	Goal       string
	ArchivedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type TaskStore struct {
	queries *models.Queries
}

// NewTaskStore builds a task store over a pool or transaction.
func NewTaskStore(db store.DBTX) *TaskStore {
	return &TaskStore{queries: models.New(db)}
}

func fromRow(row *models.Task) Task {
	return Task{
		ID:         row.ID,
		WorkflowID: row.WorkflowID,
		Slug:       row.Slug,
		Title:      row.Title,
		Goal:       row.Goal,
		ArchivedAt: row.ArchivedAt,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

type CreateParams struct {
	WorkflowID string
	Slug       string
	Title      string
	Goal       string
}

// Create writes a new task, refusing a slug that is malformed or already used.
func (s *TaskStore) Create(ctx context.Context, params CreateParams) (*Task, error) {
	if !slugPattern.MatchString(params.Slug) {
		return nil, fmt.Errorf("%w: %q", ErrBadSlug, params.Slug)
	}

	row, err := s.queries.CreateTask(ctx, models.CreateTaskParams{
		WorkflowID: params.WorkflowID,
		Slug:       params.Slug,
		Title:      params.Title,
		Goal:       params.Goal,
	})
	if db.IsUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a task is already named %q", store.ErrNameTaken, params.Slug)
	}
	if db.IsForeignKeyViolation(err, "tasks_workflow_id_fkey") {
		return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, params.WorkflowID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create task %q: %w", params.Slug, err)
	}

	created := fromRow(row)

	return &created, nil
}

// Get returns one task by id, archived or not.
func (s *TaskStore) Get(ctx context.Context, id string) (*Task, error) {
	if !store.IsID(id) {
		return nil, fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	row, err := s.queries.GetTaskByID(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task %q: %w", id, err)
	}

	got := fromRow(row)

	return &got, nil
}

// GetBySlug returns one task by its slug, archived or not.
func (s *TaskStore) GetBySlug(ctx context.Context, slug string) (*Task, error) {
	row, err := s.queries.GetTaskBySlug(ctx, slug)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: task %q", store.ErrNotFound, slug)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task %q: %w", slug, err)
	}

	got := fromRow(row)

	return &got, nil
}

// List returns a workflow's live or archived tasks, the newest first.
func (s *TaskStore) List(ctx context.Context, workflowID string, archived bool) ([]Task, error) {
	if !store.IsID(workflowID) {
		return nil, nil
	}

	rows, err := s.queries.ListTasks(ctx, models.ListTasksParams{WorkflowID: workflowID, Archived: archived})
	if err != nil {
		return nil, fmt.Errorf("failed to list the tasks of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(rows, fromRow), nil
}

// Archive takes a task out of live listings, leaving one already archived as it was.
func (s *TaskStore) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	archived, err := s.queries.ArchiveTask(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to archive task %q: %w", id, err)
	}

	if archived > 0 {
		return nil
	}

	_, err = s.Get(ctx, id)

	return err
}

// ArchiveIdle archives each of the tasks no live agent works on any longer.
func (s *TaskStore) ArchiveIdle(ctx context.Context, ids []string) error {
	if err := s.queries.ArchiveIdleTasks(ctx, ids); err != nil {
		return fmt.Errorf("failed to archive %d idle tasks: %w", len(ids), err)
	}

	return nil
}

// Restore returns archived tasks to live listings, leaving live ones as they were.
func (s *TaskStore) Restore(ctx context.Context, ids []string) error {
	if err := s.queries.RestoreTasks(ctx, ids); err != nil {
		return fmt.Errorf("failed to restore %d tasks: %w", len(ids), err)
	}

	return nil
}

// IsSlugTaken reports whether any task, archived ones included, already holds a slug.
func (s *TaskStore) IsSlugTaken(ctx context.Context, slug string) (bool, error) {
	taken, err := s.queries.IsTaskSlugTaken(ctx, slug)
	if err != nil {
		return false, fmt.Errorf("failed to check whether task slug %q is taken: %w", slug, err)
	}

	return taken, nil
}

// LockSlugs makes other transactions wait to pick a slug until this one commits, so two agents created at once never pick the same one.
func (s *TaskStore) LockSlugs(ctx context.Context) error {
	if err := s.queries.LockTaskSlugs(ctx); err != nil {
		return fmt.Errorf("failed to lock the task slugs: %w", err)
	}

	return nil
}
