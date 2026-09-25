// Package workflow stores the workflows agents are filed under, each carrying the working
// directory, flags, shared context and provisioning its agents start from.
package workflow

import (
	"time"

	"komrad/db/models"
	"komrad/internal/store"
)

type Workflow struct {
	ID         string
	Name       string
	Context    string
	Harness    string
	Cwd        string
	Flags      string
	Worktree   WorktreeConfig
	Hooks      []Hook
	ArchivedAt *time.Time
	DeletingAt *time.Time
	UpdatedAt  time.Time
}

type Config struct {
	Name     string
	Context  string
	Harness  string
	Cwd      string
	Flags    string
	Worktree WorktreeConfig
	Hooks    []Hook
}

// Config is what an update writes, being every field of a workflow but its id.
func (w Workflow) Config() Config {
	return Config{
		Name:     w.Name,
		Context:  w.Context,
		Harness:  w.Harness,
		Cwd:      w.Cwd,
		Flags:    w.Flags,
		Worktree: w.Worktree,
		Hooks:    w.Hooks,
	}
}

// Workflow is the config under an existing id, ready to be written back.
func (c Config) Workflow(id string) Workflow {
	return Workflow{
		ID:       id,
		Name:     c.Name,
		Context:  c.Context,
		Harness:  c.Harness,
		Cwd:      c.Cwd,
		Flags:    c.Flags,
		Worktree: c.Worktree,
		Hooks:    c.Hooks,
	}
}

type WorktreeConfig struct {
	Enabled      bool
	BaseBranch   string
	Dir          string
	CopyPaths    []string
	SymlinkPaths []string
	DiffExclude  []string
}

type WorkflowStore struct {
	queries *models.Queries
}

// NewWorkflowStore builds a workflow store over db, which is either the pool or a transaction.
func NewWorkflowStore(db store.DBTX) *WorkflowStore {
	return &WorkflowStore{queries: models.New(db)}
}

func orEmpty(paths []string) []string {
	if paths == nil {
		return []string{}
	}

	return paths
}

func workflowFromRow(w models.Workflow, cfg models.WorkflowConfig, prov models.WorkflowProvisionerConfig, hooks []Hook) Workflow {
	return Workflow{
		ID:      w.ID,
		Name:    w.Name,
		Context: w.Context,
		Harness: cfg.Harness,
		Cwd:     cfg.Cwd,
		Flags:   cfg.Flags,
		Worktree: WorktreeConfig{
			Enabled:      prov.WorktreeEnabled,
			BaseBranch:   prov.WorktreeBaseBranch,
			Dir:          prov.WorktreeDir,
			CopyPaths:    prov.WorktreeCopyPaths,
			SymlinkPaths: prov.WorktreeSymlinkPaths,
			DiffExclude:  prov.WorktreeDiffExclude,
		},
		Hooks:      hooks,
		ArchivedAt: w.ArchivedAt,
		DeletingAt: w.DeletingAt,
		UpdatedAt:  w.UpdatedAt,
	}
}
