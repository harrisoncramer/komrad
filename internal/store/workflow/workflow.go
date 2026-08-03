// Package workflow persists the named buckets agents are grouped into, the
// workflows table and the config row holding the defaults new agents inherit.
//
// Deleting a workflow also has to do something about the agents inside it, and
// those live in the agent package. The caller composes both inside one transaction
// rather than this package reaching across.
package workflow

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

// Workflow is a named bucket of related agents. It runs nothing. Its default
// harness, cwd, and flags are the blueprint prefilled into the new-agent form for
// agents created inside it.
type Workflow struct {
	ID   string
	Name string
	// Context is free-text durable state for the workflow, edited by the user.
	Context string
	// Harness names the CLI agents in this workflow run, empty when they take
	// whatever the user configured as their default.
	Harness string
	Cwd     string
	Flags   string
	// SetupAction names the action run before an agent in this workflow starts, and
	// CleanupAction the one run once one is deleted. Both are blank for none, and both
	// name a file in the actions directory, so nothing here can promise it still exists.
	SetupAction   string
	CleanupAction string
	// ArchivedAt is when the workflow was put away, nil while it is live. The archive draws
	// an archived header differently from a live one, so it has to tell them apart.
	ArchivedAt *time.Time
	// UpdatedAt is when the row last changed, for display. The listing sorts by
	// id instead, so an edit never moves a workflow out from under the reader.
	UpdatedAt time.Time
}

// Config is the editable blueprint of a workflow. Its display label, its free-text
// context, the actions run around its agents' lives, and the harness, cwd, and flags
// seeded into agents created inside it.
type Config struct {
	Name          string
	Context       string
	Harness       string
	Cwd           string
	Flags         string
	SetupAction   string
	CleanupAction string
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

// List returns every live workflow, newest first.
func (s *Store) List(ctx context.Context) ([]Workflow, error) {
	rows, err := s.queries.ListWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	workflows := make([]Workflow, 0, len(rows))
	for _, row := range rows {
		workflows = append(workflows, workflowFromRow(row.Workflow, row.Harness, row.Cwd, row.Flags))
	}

	return workflows, nil
}

// Cursor locates a workflow within a listing. The id is the sort key and is unique,
// so it is the whole cursor.
type Cursor struct {
	ID string `json:"i"`
}

// Validate refuses a cursor holding anything but an id, since the column it is
// compared against is a uuid and would fail the query rather than match nothing.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// ListPage returns one page of workflows, newest first, resuming after cursor when one is
// given. archived picks the archived workflows or the live ones and never both.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor, archived bool) ([]Workflow, error) {
	params := models.ListWorkflowsPageParams{Lim: int32(limit), Archived: archived}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListWorkflowsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of workflows: %w", err)
	}

	workflows := make([]Workflow, 0, len(rows))
	for _, row := range rows {
		workflows = append(workflows, workflowFromRow(row.Workflow, row.Harness, row.Cwd, row.Flags))
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
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	workflow := workflowFromRow(row.Workflow, row.Harness, row.Cwd, row.Flags)
	return &workflow, nil
}

// Create inserts a workflow together with the config row holding the harness, cwd,
// and flags new agents inside it inherit, and returns it. Both rows must land
// together, so a caller outside a transaction gets one through InTx.
func (s *Store) Create(ctx context.Context, cfg Config) (*Workflow, error) {
	configID, err := s.queries.InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{
		Harness: cfg.Harness,
		Cwd:     cfg.Cwd,
		Flags:   cfg.Flags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to insert config for workflow %q: %w", cfg.Name, err)
	}

	row, err := s.queries.InsertWorkflow(ctx, models.InsertWorkflowParams{
		Name:             cfg.Name,
		Context:          cfg.Context,
		SetupAction:      cfg.SetupAction,
		CleanupAction:    cfg.CleanupAction,
		WorkflowConfigID: configID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	workflow := workflowFromRow(*row, cfg.Harness, cfg.Cwd, cfg.Flags)
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

// Update rewrites a workflow's display name, its free-text context, the actions
// around its agents' lives, and its linked config of harness, cwd, and flags, keyed
// by id. The writes must land together, so a caller outside a transaction gets one
// through InTx.
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

	if err := s.queries.SetWorkflowActions(ctx, models.SetWorkflowActionsParams{ID: id, SetupAction: cfg.SetupAction, CleanupAction: cfg.CleanupAction}); err != nil {
		return fmt.Errorf("failed to update actions for workflow %q: %w", id, err)
	}

	if err := s.queries.UpdateWorkflowConfig(ctx, models.UpdateWorkflowConfigParams{WorkflowID: id, Harness: cfg.Harness, Cwd: cfg.Cwd, Flags: cfg.Flags}); err != nil {
		return fmt.Errorf("failed to update config for workflow %q: %w", id, err)
	}

	return nil
}

// Archive hides a workflow from the panel without dropping the row. What becomes
// of the agents inside it is the caller's to decide, in the same transaction. They
// are either unassigned or archived alongside it, so no agent is left stranded
// behind a hidden workflow.
func (s *Store) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkWorkflowArchived(ctx, id); err != nil {
		return fmt.Errorf("failed to archive workflow %q: %w", id, err)
	}

	return nil
}

// Restore puts an archived workflow back in the panel. The agents inside it are the
// caller's to bring back in the same transaction, since a workflow listed without them
// would read as empty while the archive still holds its runs.
func (s *Store) Restore(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkWorkflowRestored(ctx, id); err != nil {
		return fmt.Errorf("failed to restore workflow %q: %w", id, err)
	}

	return nil
}

// GetIncludingArchived reads a workflow by id whether or not it is archived, nil when
// none has that id. It is for the reads that only want what the row says, since the
// listings the panel builds are what archiving is meant to hide it from.
func (s *Store) GetIncludingArchived(ctx context.Context, id string) (*Workflow, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.GetWorkflowIncludingArchived(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	workflow := workflowFromRow(row.Workflow, row.Harness, row.Cwd, row.Flags)

	return &workflow, nil
}

// workflowFromRow builds a Workflow from a workflow row and its joined config.
func workflowFromRow(w models.Workflow, harness, cwd, flags string) Workflow {
	return Workflow{
		ID:            w.ID,
		Name:          w.Name,
		Context:       w.Context,
		Harness:       harness,
		Cwd:           cwd,
		Flags:         flags,
		SetupAction:   w.SetupAction,
		CleanupAction: w.CleanupAction,
		ArchivedAt:    w.ArchivedAt,
		UpdatedAt:     w.UpdatedAt,
	}
}
