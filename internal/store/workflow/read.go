package workflow

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
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

// ByName is the order every workflow listing is read in, matched by the ORDER BY in
// db/queries/workflow.sql, which sorts under the C collation because the database's own
// ignores punctuation and no Go comparison does.
func ByName(a, b Workflow) int {
	return cmp.Or(
		strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
		strings.Compare(a.ID, b.ID),
	)
}

type Cursor struct {
	Name string `json:"n"`
	ID   string `json:"i"`
}

// Validate refuses a cursor holding anything but an id.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
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
func (s *WorkflowStore) Get(ctx context.Context, id string) (*Workflow, error) {
	return s.get(ctx, id, false)
}

// GetIncludingArchived reads a workflow by id whether or not it is archived, returning
// ErrNotFound when none has that id.
func (s *WorkflowStore) GetIncludingArchived(ctx context.Context, id string) (*Workflow, error) {
	return s.get(ctx, id, true)
}

func (s *WorkflowStore) get(ctx context.Context, id string, includeArchived bool) (*Workflow, error) {
	if !store.IsID(id) {
		return nil, store.ErrNotFound.Saying("no workflow with id %q", id)
	}

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

	prompts, err := s.promptsFor(ctx, id)
	if err != nil {
		return nil, err
	}

	workflow := workflowFromRow(row.Workflow, row.WorkflowConfig, row.WorkflowProvisionerConfig, hooks)
	workflow.Prompts = prompts

	return &workflow, nil
}
