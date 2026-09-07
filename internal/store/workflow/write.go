package workflow

import (
	"context"
	"fmt"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
)

// Create inserts a workflow together with the config rows holding what new agents inherit
// and the provisioning of their directories, all of which must land together so a caller
// outside a transaction gets one through InTx.
func (s *Store) Create(ctx context.Context, cfg Config) (*Workflow, error) {
	config, err := s.queries.InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{
		Harness: cfg.Harness,
		Cwd:     cfg.Cwd,
		Flags:   cfg.Flags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to insert config for workflow %q: %w", cfg.Name, err)
	}

	provisioner, err := s.queries.InsertWorkflowProvisionerConfig(ctx, models.InsertWorkflowProvisionerConfigParams{
		WorktreeEnabled:      cfg.Worktree.Enabled,
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeDir:          cfg.Worktree.Dir,
		WorktreeCopyPaths:    orEmpty(cfg.Worktree.CopyPaths),
		WorktreeSymlinkPaths: orEmpty(cfg.Worktree.SymlinkPaths),
		WorktreeDiffExclude:  orEmpty(cfg.Worktree.DiffExclude),
		ArchiveOnPrClosed:    cfg.ArchiveOnPRClosed,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to insert the provisioner config for workflow %q: %w", cfg.Name, err)
	}

	row, err := s.queries.InsertWorkflow(ctx, models.InsertWorkflowParams{
		Name:                cfg.Name,
		Context:             cfg.Context,
		WorkflowConfigID:    config.ID,
		ProvisionerConfigID: provisioner.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	if err := s.setHooks(ctx, row.ID, cfg.Hooks); err != nil {
		return nil, err
	}

	workflow := workflowFromRow(*row, *config, *provisioner, cfg.Hooks)

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

// Update rewrites a workflow's name, context, hooks, and linked config, keyed by id, and
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

	if err := s.queries.SetWorkflowProvisioner(ctx, models.SetWorkflowProvisionerParams{
		WorkflowID:           id,
		WorktreeEnabled:      cfg.Worktree.Enabled,
		WorktreeBaseBranch:   cfg.Worktree.BaseBranch,
		WorktreeDir:          cfg.Worktree.Dir,
		WorktreeCopyPaths:    orEmpty(cfg.Worktree.CopyPaths),
		WorktreeSymlinkPaths: orEmpty(cfg.Worktree.SymlinkPaths),
		WorktreeDiffExclude:  orEmpty(cfg.Worktree.DiffExclude),
		ArchiveOnPrClosed:    cfg.ArchiveOnPRClosed,
	}); err != nil {
		return fmt.Errorf("failed to update the provisioner for workflow %q: %w", id, err)
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

	if err := s.queries.DeleteWorkflowProvisionerConfig(ctx, ids.ProvisionerConfigID); err != nil {
		return fmt.Errorf("failed to delete the provisioner config for workflow %q: %w", id, err)
	}

	return nil
}
