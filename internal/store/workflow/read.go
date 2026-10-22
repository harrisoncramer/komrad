package workflow

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"

	"github.com/google/uuid"
)

// ListWorktreeDirs returns the directories the workflows cut their worktrees in.
func (s *WorkflowStore) ListWorktreeDirs(ctx context.Context) ([]string, error) {
	dirs, err := s.queries.ListWorkflowWorktreeDirs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the directories workflows cut worktrees in: %w", err)
	}

	return dirs, nil
}

// List returns every workflow the live listing holds, by name.
func (s *WorkflowStore) List(ctx context.Context) ([]Workflow, error) {
	watched, err := s.ListWatched(ctx, nil)
	if err != nil {
		return nil, err
	}

	return xslice.Filter(watched, func(w Workflow) bool { return w.ArchivedAt == nil }), nil
}

// byName is the order every workflow listing is read in, matched by the ORDER BY in
// db/queries/workflow.sql, which sorts under the C collation because the database's own
// ignores punctuation and no Go comparison does.
func byName(a, b Workflow) int {
	return cmp.Or(
		strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
		bytes.Compare(a.ID[:], b.ID[:]),
	)
}

type Cursor struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

// Validate refuses a cursor holding anything but an id.
func (c Cursor) Validate() error {
	if c.ID == uuid.Nil {
		return errors.New("cursor names no row")
	}

	return nil
}

// ListPage returns one page of workflows by name, resuming after cursor when one is given,
// over the archived workflows or the live ones and never both.
func (s *WorkflowStore) ListPage(ctx context.Context, limit int64, cursor *Cursor, archived bool) ([]Workflow, error) {
	params := models.ListWorkflowPageIDsParams{Lim: int32(limit), Archived: archived}
	if cursor != nil {
		params.CursorName = &cursor.Name
		params.CursorID = &cursor.ID
	}

	ids, err := s.queries.ListWorkflowPageIDs(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of workflows: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	return s.ListWatched(ctx, ids)
}

// Get returns a live workflow by id, or ErrNotFound when none has that id.
func (s *WorkflowStore) Get(ctx context.Context, id uuid.UUID) (*Workflow, error) {
	return s.get(ctx, id, false)
}

// GetIncludingArchived reads a workflow by id whether or not it is archived, returning
// ErrNotFound when none has that id.
func (s *WorkflowStore) GetIncludingArchived(ctx context.Context, id uuid.UUID) (*Workflow, error) {
	return s.get(ctx, id, true)
}

func (s *WorkflowStore) get(ctx context.Context, id uuid.UUID, includeArchived bool) (*Workflow, error) {
	row, err := s.queries.GetWorkflow(ctx, models.GetWorkflowParams{ID: id, IncludeArchived: includeArchived})
	if db.IsNoRows(err) {
		return nil, store.ErrNotFound.Saying("no workflow with id %q", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	hooks, err := s.hooksFor(ctx, id)
	if err != nil {
		return nil, err
	}

	prompts, err := s.eventPromptsFor(ctx, id)
	if err != nil {
		return nil, err
	}

	workflow := workflowFromRow(workflowFromRowParams{
		Workflow:     row.Workflow,
		Config:       row.WorkflowConfig,
		Provisioner:  row.WorkflowProvisionerConfig,
		Hooks:        hooks,
		EventPrompts: prompts,
	})

	return &workflow, nil
}
