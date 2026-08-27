package workflow

import (
	"context"
	"fmt"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

type Workflow struct {
	ID   string
	Name string

	Context string

	Harness string
	Cwd     string
	Flags   string

	SetupAction   string
	CleanupAction string

	Worktree WorktreeConfig

	ArchivedAt *time.Time

	UpdatedAt time.Time
}

type Config struct {
	Name          string
	Context       string
	Harness       string
	Cwd           string
	Flags         string
	SetupAction   string
	CleanupAction string
	Worktree      WorktreeConfig
}

type WorktreeConfig struct {
	Enabled bool

	BaseBranch string

	CopyPaths []string

	SymlinkPaths []string

	DiffExclude []string

	ArchiveOnPRClosed bool
}

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

	return xslice.Map(rows, func(row *models.ListWorkflowsRow) Workflow {
		return workflowFromRow(row.Workflow, workflowConfigFields{Harness: row.Harness, Cwd: row.Cwd, Flags: row.Flags}, actionConfigFields{
			SetupAction:               row.SetupAction,
			CleanupAction:             row.CleanupAction,
			WorktreeEnabled:           row.WorktreeEnabled,
			WorktreeBaseBranch:        row.WorktreeBaseBranch,
			WorktreeCopyPaths:         row.WorktreeCopyPaths,
			WorktreeSymlinkPaths:      row.WorktreeSymlinkPaths,
			WorktreeDiffExclude:       row.WorktreeDiffExclude,
			WorktreeArchiveOnPRClosed: row.WorktreeArchiveOnPrClosed,
		})
	}), nil
}

type Cursor struct {
	ID string `json:"i"`
}

// Validate refuses a cursor holding anything but an id, which is what the listing pages by.
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

	return xslice.Map(rows, func(row *models.ListWorkflowsPageRow) Workflow {
		return workflowFromRow(row.Workflow, workflowConfigFields{Harness: row.Harness, Cwd: row.Cwd, Flags: row.Flags}, actionConfigFields{
			SetupAction:               row.SetupAction,
			CleanupAction:             row.CleanupAction,
			WorktreeEnabled:           row.WorktreeEnabled,
			WorktreeBaseBranch:        row.WorktreeBaseBranch,
			WorktreeCopyPaths:         row.WorktreeCopyPaths,
			WorktreeSymlinkPaths:      row.WorktreeSymlinkPaths,
			WorktreeDiffExclude:       row.WorktreeDiffExclude,
			WorktreeArchiveOnPRClosed: row.WorktreeArchiveOnPrClosed,
		})
	}), nil
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

	workflow := workflowFromRow(row.Workflow, workflowConfigFields{Harness: row.Harness, Cwd: row.Cwd, Flags: row.Flags}, actionConfigFields{
		SetupAction:               row.SetupAction,
		CleanupAction:             row.CleanupAction,
		WorktreeEnabled:           row.WorktreeEnabled,
		WorktreeBaseBranch:        row.WorktreeBaseBranch,
		WorktreeCopyPaths:         row.WorktreeCopyPaths,
		WorktreeSymlinkPaths:      row.WorktreeSymlinkPaths,
		WorktreeDiffExclude:       row.WorktreeDiffExclude,
		WorktreeArchiveOnPRClosed: row.WorktreeArchiveOnPrClosed,
	})
	return &workflow, nil
}

// Create inserts a workflow together with the config row holding the harness, cwd, and
// flags new agents inside it inherit, and the action config row holding its setup and
// cleanup actions and worktree settings, and returns it. All three rows must land
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

	actionConfigID, err := s.queries.InsertWorkflowActionConfig(ctx, models.InsertWorkflowActionConfigParams{
		SetupAction:               cfg.SetupAction,
		CleanupAction:             cfg.CleanupAction,
		WorktreeEnabled:           cfg.Worktree.Enabled,
		WorktreeBaseBranch:        cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:         orEmpty(cfg.Worktree.CopyPaths),
		WorktreeSymlinkPaths:      orEmpty(cfg.Worktree.SymlinkPaths),
		WorktreeDiffExclude:       orEmpty(cfg.Worktree.DiffExclude),
		WorktreeArchiveOnPrClosed: cfg.Worktree.ArchiveOnPRClosed,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to insert action config for workflow %q: %w", cfg.Name, err)
	}

	row, err := s.queries.InsertWorkflow(ctx, models.InsertWorkflowParams{
		Name:             cfg.Name,
		Context:          cfg.Context,
		WorkflowConfigID: configID,
		ActionConfigID:   actionConfigID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	workflow := workflowFromRow(*row, workflowConfigFields{Harness: cfg.Harness, Cwd: cfg.Cwd, Flags: cfg.Flags}, actionConfigFields{
		SetupAction:               cfg.SetupAction,
		CleanupAction:             cfg.CleanupAction,
		WorktreeEnabled:           cfg.Worktree.Enabled,
		WorktreeBaseBranch:        cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:         orEmpty(cfg.Worktree.CopyPaths),
		WorktreeSymlinkPaths:      orEmpty(cfg.Worktree.SymlinkPaths),
		WorktreeDiffExclude:       orEmpty(cfg.Worktree.DiffExclude),
		WorktreeArchiveOnPRClosed: cfg.Worktree.ArchiveOnPRClosed,
	})
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

	if err := s.queries.SetWorkflowActions(ctx, models.SetWorkflowActionsParams{
		WorkflowID:                id,
		SetupAction:               cfg.SetupAction,
		CleanupAction:             cfg.CleanupAction,
		WorktreeEnabled:           cfg.Worktree.Enabled,
		WorktreeBaseBranch:        cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:         orEmpty(cfg.Worktree.CopyPaths),
		WorktreeSymlinkPaths:      orEmpty(cfg.Worktree.SymlinkPaths),
		WorktreeDiffExclude:       orEmpty(cfg.Worktree.DiffExclude),
		WorktreeArchiveOnPrClosed: cfg.Worktree.ArchiveOnPRClosed,
	}); err != nil {
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

// Delete permanently removes a workflow row along with the config rows it alone owns, which
// nothing else references once the workflow itself is gone. An agent or a schedule still
// pointing at the workflow blocks the delete at the database level, since both hold it with a
// restricting foreign key.
func (s *Store) Delete(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	ids, err := s.queries.DeleteWorkflow(ctx, id)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to delete workflow %q: %w", id, err)
	}

	if err := s.queries.DeleteWorkflowConfig(ctx, ids.WorkflowConfigID); err != nil {
		return fmt.Errorf("failed to delete config for workflow %q: %w", id, err)
	}

	if err := s.queries.DeleteWorkflowActionConfig(ctx, ids.ActionConfigID); err != nil {
		return fmt.Errorf("failed to delete action config for workflow %q: %w", id, err)
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

	workflow := workflowFromRow(row.Workflow, workflowConfigFields{Harness: row.Harness, Cwd: row.Cwd, Flags: row.Flags}, actionConfigFields{
		SetupAction:               row.SetupAction,
		CleanupAction:             row.CleanupAction,
		WorktreeEnabled:           row.WorktreeEnabled,
		WorktreeBaseBranch:        row.WorktreeBaseBranch,
		WorktreeCopyPaths:         row.WorktreeCopyPaths,
		WorktreeSymlinkPaths:      row.WorktreeSymlinkPaths,
		WorktreeDiffExclude:       row.WorktreeDiffExclude,
		WorktreeArchiveOnPRClosed: row.WorktreeArchiveOnPrClosed,
	})

	return &workflow, nil
}

func orEmpty(paths []string) []string {
	if paths == nil {
		return []string{}
	}

	return paths
}

type workflowConfigFields struct {
	Harness string
	Cwd     string
	Flags   string
}

type actionConfigFields struct {
	SetupAction          string
	CleanupAction        string
	WorktreeEnabled      bool
	WorktreeBaseBranch   string
	WorktreeCopyPaths    []string
	WorktreeSymlinkPaths []string
	WorktreeDiffExclude  []string

	WorktreeArchiveOnPRClosed bool
}

func workflowFromRow(w models.Workflow, cfg workflowConfigFields, ac actionConfigFields) Workflow {
	return Workflow{
		ID:            w.ID,
		Name:          w.Name,
		Context:       w.Context,
		Harness:       cfg.Harness,
		Cwd:           cfg.Cwd,
		Flags:         cfg.Flags,
		SetupAction:   ac.SetupAction,
		CleanupAction: ac.CleanupAction,
		Worktree: WorktreeConfig{
			Enabled:           ac.WorktreeEnabled,
			BaseBranch:        ac.WorktreeBaseBranch,
			CopyPaths:         ac.WorktreeCopyPaths,
			SymlinkPaths:      ac.WorktreeSymlinkPaths,
			DiffExclude:       ac.WorktreeDiffExclude,
			ArchiveOnPRClosed: ac.WorktreeArchiveOnPRClosed,
		},
		ArchivedAt: w.ArchivedAt,
		UpdatedAt:  w.UpdatedAt,
	}
}
