package render

import (
	workflowstore "weave/internal/store/workflow"
	workflowv1 "weave/proto/gen/workflow/v1"
)

// WorktreeConfigFromProto reads a worktree config off the wire into the store's own shape.
func WorktreeConfigFromProto(w *workflowv1.WorktreeConfig) workflowstore.WorktreeConfig {
	return workflowstore.WorktreeConfig{
		Enabled:      w.GetEnabled(),
		BaseBranch:   w.GetBaseBranch(),
		Dir:          w.GetDir(),
		CopyPaths:    w.GetCopyPaths(),
		SymlinkPaths: w.GetSymlinkPaths(),
		DiffExclude:  w.GetDiffExclude(),
	}
}
