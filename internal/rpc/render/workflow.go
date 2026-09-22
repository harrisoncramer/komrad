package render

import (
	"weave/lib/dates"
	"weave/lib/xslice"
	"weave/lib/xstr"

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

// WorktreeConfigToProto writes a worktree config in the shape it crosses the wire in.
func WorktreeConfigToProto(w workflowstore.WorktreeConfig) *workflowv1.WorktreeConfig {
	return &workflowv1.WorktreeConfig{
		Enabled:      w.Enabled,
		BaseBranch:   w.BaseBranch,
		Dir:          w.Dir,
		CopyPaths:    w.CopyPaths,
		SymlinkPaths: w.SymlinkPaths,
		DiffExclude:  w.DiffExclude,
	}
}

// WorkflowOptions is how much of a workflow is drawn.
type WorkflowOptions struct {
	// ContextHead draws the opening of the context rather than the whole of it when set.
	ContextHead int
}

// Workflow draws one workflow as the wire carries it.
func Workflow(w workflowstore.Workflow, options WorkflowOptions) *workflowv1.Workflow {
	sharedContext, contextTruncated := w.Context, false
	if options.ContextHead > 0 {
		sharedContext, contextTruncated = xstr.Head(w.Context, options.ContextHead)
	}

	return &workflowv1.Workflow{
		Id:               w.ID,
		Name:             w.Name,
		Context:          sharedContext,
		Harness:          w.Harness,
		Cwd:              w.Cwd,
		Flags:            w.Flags,
		ArchivedAt:       dates.StampPtr(w.ArchivedAt),
		ContextTruncated: contextTruncated,
		Worktree:         WorktreeConfigToProto(w.Worktree),
		Hooks: xslice.Map(w.Hooks, func(hook workflowstore.Hook) *workflowv1.WorkflowHook {
			return &workflowv1.WorkflowHook{Event: hook.Event, Script: hook.Script}
		}),
	}
}
