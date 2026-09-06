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

type workflowConfigFields struct {
	Harness string
	Cwd     string
	Flags   string
}

type provisionerConfigFields struct {
	WorktreeEnabled      bool
	WorktreeBaseBranch   string
	WorktreeDir          string
	WorktreeCopyPaths    []string
	WorktreeSymlinkPaths []string
	WorktreeDiffExclude  []string
	ArchiveOnPRClosed    bool
}

func workflowFromRow(w models.Workflow, cfg workflowConfigFields, pc provisionerConfigFields, hooks []Hook) Workflow {
	return Workflow{
		ID:      w.ID,
		Name:    w.Name,
		Context: w.Context,
		Harness: cfg.Harness,
		Cwd:     cfg.Cwd,
		Flags:   cfg.Flags,
		Worktree: WorktreeConfig{
			Enabled:      pc.WorktreeEnabled,
			BaseBranch:   pc.WorktreeBaseBranch,
			Dir:          pc.WorktreeDir,
			CopyPaths:    pc.WorktreeCopyPaths,
			SymlinkPaths: pc.WorktreeSymlinkPaths,
			DiffExclude:  pc.WorktreeDiffExclude,
		},
		Hooks:             hooks,
		ArchiveOnPRClosed: pc.ArchiveOnPRClosed,
		ArchivedAt:        w.ArchivedAt,
		UpdatedAt:         w.UpdatedAt,
	}
}
