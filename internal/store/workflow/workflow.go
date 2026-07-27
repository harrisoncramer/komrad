// Package workflow persists the named buckets agents are grouped into: the
// workflows table and the config row holding the defaults new agents inherit.
//
// Deleting a workflow also has to do something about the agents inside it, but
// those live in the session package. The caller composes both inside one
// transaction rather than this package reaching across.
package workflow

import (
	"context"
	"fmt"
	"time"

	"claude-agents/db/models"
	"claude-agents/internal/store"
)

// Workflow is a named bucket of related agents. It runs nothing. Its default cwd
// and flags are the blueprint prefilled into the new-agent form for agents created
// inside it.
type Workflow struct {
	ID   string
	Name string
	// Context is free-text durable state for the workflow, edited by the user.
	Context string
	Cwd     string
	Flags   string
	// UpdatedAt is what the listing sorts by, and half of the paging cursor.
	UpdatedAt time.Time
}

// Config is the editable blueprint of a workflow: its display label, its
// free-text context, and the cwd and flags seeded into agents created inside it.
type Config struct {
	Name    string
	Context string
	Cwd     string
	Flags   string
}

// Store reads and writes workflows.
type Store struct {
	queries *models.Queries
}

// New builds a workflow store over db, which is either the pool or a
// transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// List returns every live workflow, most recently updated first.
func (s *Store) List(ctx context.Context) ([]Workflow, error) {
	rows, err := s.queries.ListWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	workflows := make([]Workflow, 0, len(rows))
	for _, row := range rows {
		workflows = append(workflows, workflowFromRow(row.Workflow, row.Cwd, row.Flags))
	}

	return workflows, nil
}

// Cursor locates a workflow within a listing, the sort key plus the id that
// breaks a tie on it.
type Cursor struct {
	UpdatedAt time.Time `json:"u"`
	ID        string    `json:"i"`
}

// ListPage returns one page of live workflows, most recently updated first,
// resuming after cursor when one is given.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor) ([]Workflow, error) {
	params := models.ListWorkflowsPageParams{Lim: int32(limit)}
	if cursor != nil {
		params.CursorUpdatedAt = &cursor.UpdatedAt
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListWorkflowsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of workflows: %w", err)
	}

	workflows := make([]Workflow, 0, len(rows))
	for _, row := range rows {
		workflows = append(workflows, workflowFromRow(row.Workflow, row.Cwd, row.Flags))
	}

	return workflows, nil
}

// Get returns a workflow by id, nil when none exists, or an error for a real
// failure. No-rows is the nil case. Every other error is surfaced.
func (s *Store) Get(ctx context.Context, id string) (*Workflow, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.GetWorkflow(ctx, id)
	if store.NoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	workflow := workflowFromRow(row.Workflow, row.Cwd, row.Flags)
	return &workflow, nil
}

// Create inserts a workflow together with the config row holding the cwd and
// flags new agents inside it inherit, and returns it. Both rows must land
// together, so a caller outside a transaction gets one through InTx.
func (s *Store) Create(ctx context.Context, cfg Config) (*Workflow, error) {
	configID, err := s.queries.InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{
		Cwd:   cfg.Cwd,
		Flags: cfg.Flags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to insert config for workflow %q: %w", cfg.Name, err)
	}

	row, err := s.queries.InsertWorkflow(ctx, models.InsertWorkflowParams{
		Name:             cfg.Name,
		Context:          cfg.Context,
		WorkflowConfigID: configID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	workflow := workflowFromRow(*row, cfg.Cwd, cfg.Flags)
	return &workflow, nil
}

// Rename changes a workflow's display name, keyed by id. Nothing keys on the
// name, so this cannot collide.
func (s *Store) Rename(ctx context.Context, id, name string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.RenameWorkflow(ctx, models.RenameWorkflowParams{ID: id, Name: name}); err != nil {
		return fmt.Errorf("failed to rename workflow %q: %w", id, err)
	}

	return nil
}

// Update rewrites a workflow's display name, its free-text context, and its
// linked config of cwd and flags, keyed by id. The three writes must land
// together, so a caller outside a transaction gets one through InTx.
func (s *Store) Update(ctx context.Context, id string, cfg Config) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.RenameWorkflow(ctx, models.RenameWorkflowParams{ID: id, Name: cfg.Name}); err != nil {
		return fmt.Errorf("failed to rename workflow %q: %w", id, err)
	}

	if err := s.queries.SetWorkflowContext(ctx, models.SetWorkflowContextParams{ID: id, Context: cfg.Context}); err != nil {
		return fmt.Errorf("failed to update context for workflow %q: %w", id, err)
	}

	if err := s.queries.UpdateWorkflowConfig(ctx, models.UpdateWorkflowConfigParams{WorkflowID: id, Cwd: cfg.Cwd, Flags: cfg.Flags}); err != nil {
		return fmt.Errorf("failed to update config for workflow %q: %w", id, err)
	}

	return nil
}

// Archive hides a workflow from the panel without dropping the row. What becomes
// of the agents inside it is the caller's to decide, in the same transaction:
// they are either unassigned or archived alongside it, and either way no agent is
// left stranded behind a hidden workflow.
func (s *Store) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkWorkflowDeleted(ctx, id); err != nil {
		return fmt.Errorf("failed to archive workflow %q: %w", id, err)
	}

	return nil
}

// workflowFromRow builds a Workflow from a workflow row and its joined config.
func workflowFromRow(w models.Workflow, cwd, flags string) Workflow {
	return Workflow{
		ID:        w.ID,
		Name:      w.Name,
		Context:   w.Context,
		Cwd:       cwd,
		Flags:     flags,
		UpdatedAt: w.UpdatedAt,
	}
}
