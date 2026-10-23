package record

import (
	"strings"

	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
)

// WorkflowConfigFields is the rows a record shows for how a workflow launches its agents.
func WorkflowConfigFields(w panel.Workflow) []Field {
	fields := []Field{
		{Label: "default agent", Value: label.OrDash(w.Harness)},
		{Label: "directory", Value: label.OrDash(w.Cwd)},
		{Label: "flags", Value: label.OrDash(w.Flags)},
	}

	if !w.Worktree.Enabled {
		return append(fields, Field{Label: "worktree", Value: "off"})
	}

	fields = append(fields,
		Field{Label: "worktree", Value: "on"},
		Field{Label: "base branch", Value: label.OrDash(w.Worktree.BaseBranch)},
	)
	if len(w.Worktree.CopyPaths) > 0 {
		fields = append(fields, Field{Label: "copy", Value: strings.Join(w.Worktree.CopyPaths, ", ")})
	}
	if len(w.Worktree.SymlinkPaths) > 0 {
		fields = append(fields, Field{Label: "symlink", Value: strings.Join(w.Worktree.SymlinkPaths, ", ")})
	}
	if w.Worktree.SetupCommand != "" {
		fields = append(fields, Field{Label: "setup", Value: w.Worktree.SetupCommand})
	}

	return fields
}
