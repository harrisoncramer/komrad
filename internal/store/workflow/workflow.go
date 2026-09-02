// Package workflow stores the workflows agents are filed under, which carry the working
// directory, flags, shared context and the provisioner their agents start from.
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

// The moments a workflow can hang a script on, each named for the event it hangs on and the
// side of it that runs, so reading the name says when it fires.
const (
	HookPreLaunch   = "pre-launch"
	HookPostLaunch  = "post-launch"
	HookPostResume  = "post-resume"
	HookPostRestore = "post-restore"
)

// HookEvents are the moments a workflow can hang a script on, in the order they fire.
func HookEvents() []string {
	return []string{HookPreLaunch, HookPostLaunch, HookPostResume, HookPostRestore}
}

// Hook is one script a workflow hangs on a moment, and a moment can hold several.
type Hook struct {
	Event  string
	Action string
}

type Workflow struct {
	ID                string
	Name              string
	Context           string
	Harness           string
	Cwd               string
	Flags             string
	ProvisionAction   string
	DeprovisionAction string
	Worktree          WorktreeConfig
	Hooks             []Hook
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
	ProvisionAction   string
	DeprovisionAction string
	Worktree          WorktreeConfig
	Hooks             []Hook
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
		ProvisionAction:   w.ProvisionAction,
		DeprovisionAction: w.DeprovisionAction,
		Worktree:          w.Worktree,
		Hooks:             w.Hooks,
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
		ProvisionAction:   c.ProvisionAction,
		DeprovisionAction: c.DeprovisionAction,
		Worktree:          c.Worktree,
		Hooks:             c.Hooks,
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

	hooks, err := s.hooksByWorkflow(ctx)
	if err != nil {
		return nil, err
	}

	return xslice.Map(rows, func(row *models.ListWorkflowsRow) Workflow {
		return workflowFromRow(row.Workflow, workflowConfigFields{
			Harness: row.Harness,
			Cwd:     row.Cwd,
			Flags:   row.Flags,
		}, actionConfigFields{
			ProvisionAction:      row.ProvisionAction,
			DeprovisionAction:    row.DeprovisionAction,
			WorktreeEnabled:      row.WorktreeEnabled,
			WorktreeBaseBranch:   row.WorktreeBaseBranch,
			WorktreeCopyPaths:    row.WorktreeCopyPaths,
			WorktreeSymlinkPaths: row.WorktreeSymlinkPaths,
			WorktreeDiffExclude:  row.WorktreeDiffExclude,
			ArchiveOnPRClosed:    row.ArchiveOnPrClosed,
		}, hooks[row.Workflow.ID])
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

	hooks, err := s.hooksByWorkflow(ctx)
	if err != nil {
		return nil, err
	}

	return xslice.Map(rows, func(row *models.ListWorkflowsPageRow) Workflow {
		return workflowFromRow(row.Workflow, workflowConfigFields{
			Harness: row.Harness,
			Cwd:     row.Cwd,
			Flags:   row.Flags,
		}, actionConfigFields{
			ProvisionAction:      row.ProvisionAction,
			DeprovisionAction:    row.DeprovisionAction,
			WorktreeEnabled:      row.WorktreeEnabled,
			WorktreeBaseBranch:   row.WorktreeBaseBranch,
			WorktreeCopyPaths:    row.WorktreeCopyPaths,
			WorktreeSymlinkPaths: row.WorktreeSymlinkPaths,
			WorktreeDiffExclude:  row.WorktreeDiffExclude,
			ArchiveOnPRClosed:    row.ArchiveOnPrClosed,
		}, hooks[row.Workflow.ID])
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

	hooks, err := s.hooksFor(ctx, id)
	if err != nil {
		return nil, err
	}

	workflow := workflowFromRow(row.Workflow, workflowConfigFields{
		Harness: row.Harness,
		Cwd:     row.Cwd,
		Flags:   row.Flags,
	}, actionConfigFields{
		ProvisionAction:      row.ProvisionAction,
		DeprovisionAction:    row.DeprovisionAction,
		WorktreeEnabled:      row.WorktreeEnabled,
		WorktreeBaseBranch:   row.WorktreeBaseBranch,
		WorktreeCopyPaths:    row.WorktreeCopyPaths,
		WorktreeSymlinkPaths: row.WorktreeSymlinkPaths,
		WorktreeDiffExclude:  row.WorktreeDiffExclude,
		ArchiveOnPRClosed:    row.ArchiveOnPrClosed,
	}, hooks)

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
		ProvisionAction:      cfg.ProvisionAction,
		DeprovisionAction:    cfg.DeprovisionAction,
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

	if err := s.setHooks(ctx, row.ID, cfg.Hooks); err != nil {
		return nil, err
	}

	workflow := workflowFromRow(*row, workflowConfigFields{
		Harness: cfg.Harness,
		Cwd:     cfg.Cwd,
		Flags:   cfg.Flags,
	}, actionConfigFields{
		ProvisionAction:      cfg.ProvisionAction,
		DeprovisionAction:    cfg.DeprovisionAction,
		WorktreeEnabled:      cfg.Worktree.Enabled,
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeCopyPaths:    orEmpty(cfg.Worktree.CopyPaths),
		WorktreeSymlinkPaths: orEmpty(cfg.Worktree.SymlinkPaths),
		WorktreeDiffExclude:  orEmpty(cfg.Worktree.DiffExclude),
		ArchiveOnPRClosed:    cfg.ArchiveOnPRClosed,
	}, cfg.Hooks)

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
		ProvisionAction:      cfg.ProvisionAction,
		DeprovisionAction:    cfg.DeprovisionAction,
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

	return s.setHooks(ctx, id, cfg.Hooks)
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

	hooks, err := s.hooksFor(ctx, id)
	if err != nil {
		return nil, err
	}

	workflow := workflowFromRow(row.Workflow, workflowConfigFields{
		Harness: row.Harness,
		Cwd:     row.Cwd,
		Flags:   row.Flags,
	}, actionConfigFields{
		ProvisionAction:      row.ProvisionAction,
		DeprovisionAction:    row.DeprovisionAction,
		WorktreeEnabled:      row.WorktreeEnabled,
		WorktreeBaseBranch:   row.WorktreeBaseBranch,
		WorktreeCopyPaths:    row.WorktreeCopyPaths,
		WorktreeSymlinkPaths: row.WorktreeSymlinkPaths,
		WorktreeDiffExclude:  row.WorktreeDiffExclude,
		ArchiveOnPRClosed:    row.ArchiveOnPrClosed,
	}, hooks)

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
	ProvisionAction      string
	DeprovisionAction    string
	WorktreeEnabled      bool
	WorktreeBaseBranch   string
	WorktreeCopyPaths    []string
	WorktreeSymlinkPaths []string
	WorktreeDiffExclude  []string
	ArchiveOnPRClosed    bool
}

// setHooks rewrites a workflow's hooks, since a caller sends back the whole list rather than
// an edit to one of them, and the position they land in is the order they were sent.
func (s *Store) setHooks(ctx context.Context, workflowID string, hooks []Hook) error {
	if err := s.queries.DeleteWorkflowHooks(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to clear hooks for workflow %q: %w", workflowID, err)
	}

	for position, hook := range hooks {
		if err := s.queries.InsertWorkflowHook(ctx, models.InsertWorkflowHookParams{
			WorkflowID: workflowID,
			Event:      hook.Event,
			Action:     hook.Action,
			Position:   int32(position),
		}); err != nil {
			return fmt.Errorf("failed to add the %s hook %q to workflow %q: %w", hook.Event, hook.Action, workflowID, err)
		}
	}

	return nil
}

func (s *Store) hooksFor(ctx context.Context, workflowID string) ([]Hook, error) {
	rows, err := s.queries.ListHooksForWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the hooks of workflow %q: %w", workflowID, err)
	}

	return xslice.Map(rows, func(row *models.ListHooksForWorkflowRow) Hook {
		return Hook{Event: row.Event, Action: row.Action}
	}), nil
}

// hooksByWorkflow reads every workflow's hooks at once, so a listing costs one query for the
// lot rather than one apiece.
func (s *Store) hooksByWorkflow(ctx context.Context) (map[string][]Hook, error) {
	rows, err := s.queries.ListWorkflowHooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow hooks: %w", err)
	}

	hooks := make(map[string][]Hook)
	for _, row := range rows {
		hooks[row.WorkflowID] = append(hooks[row.WorkflowID], Hook{Event: row.Event, Action: row.Action})
	}

	return hooks, nil
}

func workflowFromRow(w models.Workflow, cfg workflowConfigFields, ac actionConfigFields, hooks []Hook) Workflow {
	return Workflow{
		ID:                w.ID,
		Name:              w.Name,
		Context:           w.Context,
		Harness:           cfg.Harness,
		Cwd:               cfg.Cwd,
		Flags:             cfg.Flags,
		ProvisionAction:   ac.ProvisionAction,
		DeprovisionAction: ac.DeprovisionAction,
		Worktree: WorktreeConfig{
			Enabled:      ac.WorktreeEnabled,
			BaseBranch:   ac.WorktreeBaseBranch,
			CopyPaths:    ac.WorktreeCopyPaths,
			SymlinkPaths: ac.WorktreeSymlinkPaths,
			DiffExclude:  ac.WorktreeDiffExclude,
		},
		Hooks:             hooks,
		ArchiveOnPRClosed: ac.ArchiveOnPRClosed,
		ArchivedAt:        w.ArchivedAt,
		UpdatedAt:         w.UpdatedAt,
	}
}
