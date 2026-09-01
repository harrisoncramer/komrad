// Package workflow stores the workflows agents are filed under, which carry the working
// directory, flags, shared context and setup and cleanup actions their agents start from.
package workflow

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"
)

type Workflow struct {
	ID                string
	Name              string
	Context           string
	Harness           string
	Cwd               string
	Flags             string
	SetupAction       string
	CleanupAction     string
	Worktree          WorktreeConfig
	ArchiveOnPRClosed bool
	ArchivedAt        *time.Time
	UpdatedAt         time.Time
}

type Config struct {
	Name              string
	Context           string
	Harness           string
	Cwd               string
	Flags             string
	SetupAction       string
	CleanupAction     string
	Worktree          WorktreeConfig
	ArchiveOnPRClosed bool
}

// Config is what an update writes, which is every field of a workflow but its id.
func (w Workflow) Config() Config {
	return Config{
		Name:              w.Name,
		Context:           w.Context,
		Harness:           w.Harness,
		Cwd:               w.Cwd,
		Flags:             w.Flags,
		SetupAction:       w.SetupAction,
		CleanupAction:     w.CleanupAction,
		Worktree:          w.Worktree,
		ArchiveOnPRClosed: w.ArchiveOnPRClosed,
	}
}

// Workflow is the config under an existing id, ready to be written back.
func (c Config) Workflow(id string) Workflow {
	return Workflow{
		ID:                id,
		Name:              c.Name,
		Context:           c.Context,
		Harness:           c.Harness,
		Cwd:               c.Cwd,
		Flags:             c.Flags,
		SetupAction:       c.SetupAction,
		CleanupAction:     c.CleanupAction,
		Worktree:          c.Worktree,
		ArchiveOnPRClosed: c.ArchiveOnPRClosed,
	}
}

type WorktreeConfig struct {
	Enabled      bool
	BaseBranch   string
	CopyPaths    []string
	SymlinkPaths []string
	DiffExclude  []string
}

type Store struct {
	queries *models.Queries
}

// New builds a workflow store over db, which is either the pool or a
// transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// List returns every live workflow, by name.
func (s *Store) List(ctx context.Context) ([]Workflow, error) {
	rows, err := s.queries.ListWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListWorkflowsRow) Workflow {
		return workflowFromRow(row.Workflow, workflowConfigFields{
			Harness: row.Harness,
			Cwd:     row.Cwd,
			Flags:   row.Flags,
		}, actionConfigFields{
			SetupAction:          row.SetupAction,
			CleanupAction:        row.CleanupAction,
			WorktreeEnabled:      row.WorktreeEnabled,
			WorktreeBaseBranch:   row.WorktreeBaseBranch,
			WorktreeCopyPaths:    row.WorktreeCopyPaths,
			WorktreeSymlinkPaths: row.WorktreeSymlinkPaths,
			WorktreeDiffExclude:  row.WorktreeDiffExclude,
			ArchiveOnPRClosed:    row.ArchiveOnPrClosed,
		})
	}), nil
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

// Validate refuses a cursor holding anything but an id, since the name it pages by alongside
// is whatever the workflow was called.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// ListPage returns one page of workflows by name, resuming after cursor when one is given,
// over the archived workflows or the live ones and never both.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor, archived bool) ([]Workflow, error) {
	params := models.ListWorkflowsPageParams{Lim: int32(limit), Archived: archived}
	if cursor != nil {
		params.CursorName = &cursor.Name
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListWorkflowsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of workflows: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListWorkflowsPageRow) Workflow {
		return workflowFromRow(row.Workflow, workflowConfigFields{
			Harness: row.Harness,
			Cwd:     row.Cwd,
			Flags:   row.Flags,
		}, actionConfigFields{
			SetupAction:          row.SetupAction,
			CleanupAction:        row.CleanupAction,
			WorktreeEnabled:      row.WorktreeEnabled,
			WorktreeBaseBranch:   row.WorktreeBaseBranch,
			WorktreeCopyPaths:    row.WorktreeCopyPaths,
			WorktreeSymlinkPaths: row.WorktreeSymlinkPaths,
			WorktreeDiffExclude:  row.WorktreeDiffExclude,
			ArchiveOnPRClosed:    row.ArchiveOnPrClosed,
		})
	}), nil
}

// Get returns a live workflow by id, or ErrNotFound when none has that id.
func (s *Store) Get(ctx context.Context, id string) (*Workflow, error) {
	if !store.IsID(id) {
		return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, id)
	}

	row, err := s.queries.GetWorkflow(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	workflow := workflowFromRow(row.Workflow, workflowConfigFields{
		Harness: row.Harness,
		Cwd:     row.Cwd,
		Flags:   row.Flags,
	}, actionConfigFields{
		SetupAction:          row.SetupAction,
		CleanupAction:        row.CleanupAction,
		WorktreeEnabled:      row.WorktreeEnabled,
		WorktreeBaseBranch:   row.WorktreeBaseBranch,
		WorktreeCopyPaths:    row.WorktreeCopyPaths,
		WorktreeSymlinkPaths: row.WorktreeSymlinkPaths,
		WorktreeDiffExclude:  row.WorktreeDiffExclude,
		ArchiveOnPRClosed:    row.ArchiveOnPrClosed,
	})
	return &workflow, nil
}

// Create inserts a workflow together with the config rows holding what new agents inherit
// and the actions around their lives, all of which must land together so a caller outside a
// transaction gets one through InTx.
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
		SetupAction:          cfg.SetupAction,
		CleanupAction:        cfg.CleanupAction,
		WorktreeEnabled:      cfg.Worktree.Enabled,
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    orEmpty(cfg.Worktree.CopyPaths),
		WorktreeSymlinkPaths: orEmpty(cfg.Worktree.SymlinkPaths),
		WorktreeDiffExclude:  orEmpty(cfg.Worktree.DiffExclude),
		ArchiveOnPrClosed:    cfg.ArchiveOnPRClosed,
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

	workflow := workflowFromRow(*row, workflowConfigFields{
		Harness: cfg.Harness,
		Cwd:     cfg.Cwd,
		Flags:   cfg.Flags,
	}, actionConfigFields{
		SetupAction:          cfg.SetupAction,
		CleanupAction:        cfg.CleanupAction,
		WorktreeEnabled:      cfg.Worktree.Enabled,
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    orEmpty(cfg.Worktree.CopyPaths),
		WorktreeSymlinkPaths: orEmpty(cfg.Worktree.SymlinkPaths),
		WorktreeDiffExclude:  orEmpty(cfg.Worktree.DiffExclude),
		ArchiveOnPRClosed:    cfg.ArchiveOnPRClosed,
	})
	return &workflow, nil
}

// Rename changes a workflow's display name, keyed by id, and cannot collide since nothing
// keys on the name.
func (s *Store) Rename(ctx context.Context, id, name string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.RenameWorkflow(ctx, models.RenameWorkflowParams{ID: id, Name: name}); err != nil {
		return fmt.Errorf("failed to rename workflow %q: %w", id, err)
	}

	return nil
}

// Update rewrites a workflow's name, context, actions, and linked config, keyed by id, and
// the writes must land together so a caller outside a transaction gets one through InTx.
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
		WorkflowID:           id,
		SetupAction:          cfg.SetupAction,
		CleanupAction:        cfg.CleanupAction,
		WorktreeEnabled:      cfg.Worktree.Enabled,
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    orEmpty(cfg.Worktree.CopyPaths),
		WorktreeSymlinkPaths: orEmpty(cfg.Worktree.SymlinkPaths),
		WorktreeDiffExclude:  orEmpty(cfg.Worktree.DiffExclude),
		ArchiveOnPrClosed:    cfg.ArchiveOnPRClosed,
	}); err != nil {
		return fmt.Errorf("failed to update actions for workflow %q: %w", id, err)
	}

	if err := s.queries.UpdateWorkflowConfig(ctx, models.UpdateWorkflowConfigParams{
		WorkflowID: id,
		Harness:    cfg.Harness,
		Cwd:        cfg.Cwd,
		Flags:      cfg.Flags,
	}); err != nil {
		return fmt.Errorf("failed to update config for workflow %q: %w", id, err)
	}

	return nil
}

// Archive hides a workflow from the panel without dropping the row, leaving the caller to
// unassign or archive the agents inside it in the same transaction.
func (s *Store) Archive(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.MarkWorkflowArchived(ctx, id); err != nil {
		return fmt.Errorf("failed to archive workflow %q: %w", id, err)
	}

	return nil
}

// Restore puts an archived workflow back in the panel, leaving the caller to bring its agents
// back in the same transaction so it does not read as empty.
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
// an agent or a schedule still pointing at it blocks at the database level.
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

// GetIncludingArchived reads a workflow by id whether or not it is archived, returning
// ErrNotFound when none has that id.
func (s *Store) GetIncludingArchived(ctx context.Context, id string) (*Workflow, error) {
	if !store.IsID(id) {
		return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, id)
	}

	row, err := s.queries.GetWorkflowIncludingArchived(ctx, id)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	workflow := workflowFromRow(row.Workflow, workflowConfigFields{
		Harness: row.Harness,
		Cwd:     row.Cwd,
		Flags:   row.Flags,
	}, actionConfigFields{
		SetupAction:          row.SetupAction,
		CleanupAction:        row.CleanupAction,
		WorktreeEnabled:      row.WorktreeEnabled,
		WorktreeBaseBranch:   row.WorktreeBaseBranch,
		WorktreeCopyPaths:    row.WorktreeCopyPaths,
		WorktreeSymlinkPaths: row.WorktreeSymlinkPaths,
		WorktreeDiffExclude:  row.WorktreeDiffExclude,
		ArchiveOnPRClosed:    row.ArchiveOnPrClosed,
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
	ArchiveOnPRClosed    bool
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
			Enabled:      ac.WorktreeEnabled,
			BaseBranch:   ac.WorktreeBaseBranch,
			CopyPaths:    ac.WorktreeCopyPaths,
			SymlinkPaths: ac.WorktreeSymlinkPaths,
			DiffExclude:  ac.WorktreeDiffExclude,
		},
		ArchiveOnPRClosed: ac.ArchiveOnPRClosed,
		ArchivedAt:        w.ArchivedAt,
		UpdatedAt:         w.UpdatedAt,
	}
}
