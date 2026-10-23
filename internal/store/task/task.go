// Package task stores tasks, the outcomes agents are linked to and work toward.
package task

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"
	"komrad/internal/store"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/google/uuid"
)

// ErrBadSlug is a slug that is not lowercase kebab-case.
var ErrBadSlug = fail.InvalidArgument("a task slug is lowercase letters and digits joined by single hyphens")

// ErrNotesChanged is a write of a task's notes made over a version that has since been rewritten.
var ErrNotesChanged = fail.Precondition("the task's notes have been written since they were read")

// ErrNoTask is an agent on no task, or on one since deleted.
var ErrNoTask = fail.NotFound("the agent is on no task")

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Task is one outcome a person wants, which the agents linked to it work toward.
type Task struct {
	ID             uuid.UUID
	WorkflowID     uuid.UUID
	Slug           string
	Title          string
	Goal           string
	Notes          string
	NotesUpdatedAt *time.Time
	Checkout       Checkout
	// Parent is the task this one depends on, nil for a task on the workflow's base branch.
	Parent        *Parent
	Hidden        bool
	ArchivedAt    *time.Time
	DeletingAt    *time.Time
	FailureReason string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	// FailedRun is whether a run on the task failed and nobody has opened it, which only a watch reads.
	FailedRun bool
	// Documents is how many documents agents have written for the task, which only a watch reads.
	Documents int32
	// DocumentsUpdatedAt is when the newest of them was last written, nil when there are none.
	DocumentsUpdatedAt *time.Time
	// Snapshots is how many snapshots komrad has taken of the task's checkout.
	Snapshots int32
}

// GetBranch is the branch a task's checkout is on, or the one komrad names for it while it has none.
func (t Task) GetBranch() string {
	if branch := xptr.Deref(t.Checkout.Branch); branch != "" {
		return branch
	}
	if t.Checkout.AdoptedBranch != "" {
		return t.Checkout.AdoptedBranch
	}

	return "task/" + t.ID.String()
}

type TaskStore struct {
	queries *models.Queries
}

// NewTaskStore builds a task store over a pool or transaction.
func NewTaskStore(db store.DBTX) *TaskStore {
	return &TaskStore{queries: models.New(db)}
}

// fromCountedRow is a task row with how many snapshots were taken of its checkout.
func fromCountedRow(row *models.Task, snapshots int32) (*Task, error) {
	task, err := fromRow(row)
	if err != nil {
		return nil, err
	}
	task.Snapshots = snapshots

	return task, nil
}

func fromRow(row *models.Task) (*Task, error) {
	checkout, err := checkoutFrom(row)
	if err != nil {
		return nil, err
	}
	var parent *Parent
	if row.ParentTaskID != nil {
		parent, err = parentFrom(row)
		if err != nil {
			return nil, err
		}
	}

	return &Task{
		ID:             row.ID,
		WorkflowID:     row.WorkflowID,
		Slug:           row.Slug,
		Title:          row.Title,
		Goal:           row.Goal,
		Notes:          row.Notes,
		NotesUpdatedAt: row.NotesUpdatedAt,
		Checkout:       checkout,
		Parent:         parent,
		Hidden:         row.IsHidden,
		ArchivedAt:     row.ArchivedAt,
		DeletingAt:     row.DeletingAt,
		FailureReason:  row.FailureReason,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

func fromRows(rows []*models.Task) ([]Task, error) {
	return xslice.TryMap(rows, func(row *models.Task) (Task, error) {
		task, err := fromRow(row)
		if err != nil {
			return Task{}, err
		}

		return *task, nil
	})
}

type CreateParams struct {
	WorkflowID uuid.UUID
	Slug       string
	Title      string
	Goal       string
	// Parent is the task the new task depends on, nil for none.
	Parent *Parent
}

// Create writes a new task, refusing a slug that is malformed or already used.
func (s *TaskStore) Create(ctx context.Context, params CreateParams) (*Task, error) {
	if !slugPattern.MatchString(params.Slug) {
		return nil, fmt.Errorf("%w: %q", ErrBadSlug, params.Slug)
	}

	create := models.CreateTaskParams{
		WorkflowID: params.WorkflowID,
		Slug:       params.Slug,
		Title:      params.Title,
		Goal:       params.Goal,
	}
	if params.Parent != nil {
		create.ParentTaskID = &params.Parent.TaskID
		create.ForkPoint = xptr.NonEmpty(params.Parent.ForkPoint)
		create.ParentPullRequestID = params.Parent.PullRequestID
	}

	row, err := s.queries.CreateTask(ctx, create)
	if db.IsUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a task is already named %q", store.ErrNameTaken, params.Slug)
	}
	if db.IsForeignKeyViolation(err, "tasks_workflow_id_fkey") {
		return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, params.WorkflowID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create task %q: %w", params.Slug, err)
	}

	return fromRow(row)
}

// UpdateParams is the fields of a task to rewrite, each left as it is when nil.
type UpdateParams struct {
	Slug               *string
	Title              *string
	Goal               *string
	Notes              *string
	ReadNotesUpdatedAt *time.Time
}

// Update rewrites the fields of a task that params sets, refusing a slug that is malformed or held
// by another task and notes written since ReadNotesUpdatedAt.
func (s *TaskStore) Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Task, error) {
	if params.Slug != nil && !slugPattern.MatchString(*params.Slug) {
		return nil, fmt.Errorf("%w: %q", ErrBadSlug, *params.Slug)
	}

	row, err := s.queries.UpdateTask(ctx, models.UpdateTaskParams{
		ID:                 id,
		Slug:               params.Slug,
		Title:              params.Title,
		Goal:               params.Goal,
		Notes:              params.Notes,
		ReadNotesUpdatedAt: params.ReadNotesUpdatedAt,
	})
	if db.IsUniqueViolation(err) {
		return nil, fmt.Errorf("%w: a task is already named %q", store.ErrNameTaken, xptr.Deref(params.Slug))
	}
	if db.IsNoRows(err) {
		return nil, s.explainUnwritten(ctx, id, params.Notes != nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update task %q: %w", id, err)
	}

	return fromRow(row)
}

// explainUnwritten says why an update wrote no row, a task gone or being deleted or notes written since they were read.
func (s *TaskStore) explainUnwritten(ctx context.Context, id uuid.UUID, setsNotes bool) error {
	found, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if found.DeletingAt != nil || !setsNotes {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}

	return fmt.Errorf("%w: task %q", ErrNotesChanged, found.Slug)
}

// Get returns one task by id, archived or not.
func (s *TaskStore) Get(ctx context.Context, id uuid.UUID) (*Task, error) {
	row, err := s.queries.GetTaskByID(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: task %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get task %q: %w", id, err)
	}

	return fromCountedRow(&row.Task, row.Snapshots)
}

// GetLinked returns the task an agent's task id names, refusing an agent on no task.
func (s *TaskStore) GetLinked(ctx context.Context, taskID *uuid.UUID) (*Task, error) {
	if taskID == nil {
		return nil, ErrNoTask
	}

	found, err := s.Get(ctx, *taskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: task %q", ErrNoTask, *taskID)
	}
	if err != nil {
		return nil, err
	}

	return found, nil
}

// GetLinkedBranch returns the branch of the checkout an agent's task id names, empty when there is none.
func (s *TaskStore) GetLinkedBranch(ctx context.Context, taskID *uuid.UUID) (string, error) {
	task, err := s.GetLinked(ctx, taskID)
	if errors.Is(err, ErrNoTask) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return xptr.Deref(task.Checkout.Branch), nil
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

	return fromCountedRow(&row.Task, row.Snapshots)
}

// List returns a workflow's live or archived tasks, the newest first.
func (s *TaskStore) List(ctx context.Context, workflowID uuid.UUID, archived bool) ([]Task, error) {
	rows, err := s.queries.ListTasks(ctx, models.ListTasksParams{WorkflowID: workflowID, Archived: archived})
	if err != nil {
		return nil, fmt.Errorf("failed to list the tasks of workflow %q: %w", workflowID, err)
	}

	return xslice.TryMap(rows, func(row *models.ListTasksRow) (Task, error) {
		task, err := fromCountedRow(&row.Task, row.Snapshots)
		if err != nil {
			return Task{}, err
		}

		return *task, nil
	})
}

// Archive takes a task out of live listings, leaving one already archived as it was.
func (s *TaskStore) Archive(ctx context.Context, id uuid.UUID) error {
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

// ArchiveIdle archives each of the tasks no live agent works on any longer, returning those it archived.
func (s *TaskStore) ArchiveIdle(ctx context.Context, ids []uuid.UUID) ([]Task, error) {
	if err := s.queries.LockTasks(ctx, ids); err != nil {
		return nil, fmt.Errorf("failed to lock %d idle tasks: %w", len(ids), err)
	}
	rows, err := s.queries.ArchiveIdleTasks(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to archive %d idle tasks: %w", len(ids), err)
	}

	return fromRows(rows)
}

// Restore returns archived tasks to live listings, leaving live ones as they were.
func (s *TaskStore) Restore(ctx context.Context, ids []uuid.UUID) error {
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

// SetHidden hides a live task from the panel or shows it again, reporting whether a task took
// the change.
func (s *TaskStore) SetHidden(ctx context.Context, id uuid.UUID, hidden bool) (bool, error) {
	changed, err := s.queries.SetTaskHidden(ctx, models.SetTaskHiddenParams{ID: id, Hidden: hidden})
	if err != nil {
		return false, fmt.Errorf("failed to hide task %q: %w", id, err)
	}

	return changed > 0, nil
}
