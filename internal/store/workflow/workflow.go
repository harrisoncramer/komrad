// Package workflow stores the workflows agents are filed under, which carry the working
// directory, flags, shared context and the provisioner their agents start from.
package workflow

import (
	"time"

	"weave/db/models"
	"weave/internal/store"
)

type Workflow struct {
	ID                string
	Name              string
	Context           string
	Harness           string
	Cwd               string
	Flags             string
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
		Worktree:          c.Worktree,
		Hooks:             c.Hooks,
		ArchiveOnPRClosed: c.ArchiveOnPRClosed,
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

type Store struct {
	queries *models.Queries
}

// New builds a workflow store over db, which is either the pool or a
// transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

func orEmpty(paths []string) []string {
	if paths == nil {
		return []string{}
	}

	return paths
}

type workflowRow struct {
	Workflow    models.Workflow
	Config      models.WorkflowConfig
	Provisioner models.WorkflowProvisionerConfig
	Hooks       []Hook
}

func workflowFromRow(row workflowRow) Workflow {
	return Workflow{
		ID:      row.Workflow.ID,
		Name:    row.Workflow.Name,
		Context: row.Workflow.Context,
		Harness: row.Config.Harness,
		Cwd:     row.Config.Cwd,
		Flags:   row.Config.Flags,
		Worktree: WorktreeConfig{
			Enabled:      row.Provisioner.WorktreeEnabled,
			BaseBranch:   row.Provisioner.WorktreeBaseBranch,
			Dir:          row.Provisioner.WorktreeDir,
			CopyPaths:    row.Provisioner.WorktreeCopyPaths,
			SymlinkPaths: row.Provisioner.WorktreeSymlinkPaths,
			DiffExclude:  row.Provisioner.WorktreeDiffExclude,
		},
		Hooks:             row.Hooks,
		ArchiveOnPRClosed: row.Provisioner.ArchiveOnPrClosed,
		ArchivedAt:        row.Workflow.ArchivedAt,
		UpdatedAt:         row.Workflow.UpdatedAt,
	}
}
