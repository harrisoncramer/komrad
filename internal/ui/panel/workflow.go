package panel

import (
	"cmp"
	"strings"
	"time"
)

// Workflow is one workflow as the service listed it.
type Workflow struct {
	ID              string
	Name            string
	Context         string
	Harness         string
	Cwd             string
	Flags           string
	ReviewerHarness string
	ReviewerFlags   string
	Worktree        WorktreeConfig
	Hooks           []Hook
	ArchivedAt      *time.Time
}

// WorkflowConfig is every field of a workflow but its id, which is what a form fills.
type WorkflowConfig struct {
	Name            string
	Context         string
	Harness         string
	Cwd             string
	Flags           string
	ReviewerHarness string
	ReviewerFlags   string
	Worktree        WorktreeConfig
	Hooks           []Hook
}

// Config is the workflow with its id left off.
func (w Workflow) Config() WorkflowConfig {
	return WorkflowConfig{
		Name:            w.Name,
		Context:         w.Context,
		Harness:         w.Harness,
		Cwd:             w.Cwd,
		Flags:           w.Flags,
		ReviewerHarness: w.ReviewerHarness,
		ReviewerFlags:   w.ReviewerFlags,
		Worktree:        w.Worktree,
		Hooks:           w.Hooks,
	}
}

// Workflow is the config under an existing id.
func (c WorkflowConfig) Workflow(id string) Workflow {
	return Workflow{
		ID:              id,
		Name:            c.Name,
		Context:         c.Context,
		Harness:         c.Harness,
		Cwd:             c.Cwd,
		Flags:           c.Flags,
		ReviewerHarness: c.ReviewerHarness,
		ReviewerFlags:   c.ReviewerFlags,
		Worktree:        c.Worktree,
		Hooks:           c.Hooks,
	}
}

// WorktreeConfig is how a workflow provisions a directory for each of its agents.
type WorktreeConfig struct {
	Enabled      bool
	BaseBranch   string
	Dir          string
	CopyPaths    []string
	SymlinkPaths []string
	DiffExclude  []string
}

// Hook is one script a workflow hangs on a moment.
type Hook struct {
	Event  string
	Script string
}

// ByName orders workflows by name, ignoring case, and by id between two of one name.
func ByName(a, b Workflow) int {
	return cmp.Or(
		strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
		strings.Compare(a.ID, b.ID),
	)
}
