package workflow

import (
	"weave/internal/rpc/render"

	workflowstore "weave/internal/store/workflow"
	"weave/lib/dates"
	"weave/lib/xslice"
	"weave/lib/xstr"
	viewv1 "weave/proto/gen/view/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
)

type workflow struct {
	*workflowv1.Workflow
	cursor workflowstore.Cursor
}

// Cursor returns the keyset cursor locating this workflow.
func (w workflow) Cursor() (workflowstore.Cursor, error) {
	return w.cursor, nil
}

// workflowOptions is how much of a workflow renderWorkflow draws.
type workflowOptions struct {
	contextHead int
}

type workflowOption = render.LoadOption[workflowOptions]

// withContextHead draws the opening of the context rather than the whole of it.
func withContextHead(limit int) workflowOption {
	return func(o *workflowOptions) { o.contextHead = limit }
}

// workflowViewOptions turns the view a request asked for into the options renderWorkflow takes,
// so the enum stops at the handler rather than reaching the renderer.
func workflowViewOptions(view viewv1.View) []workflowOption {
	if view == viewv1.View_VIEW_FULL {
		return nil
	}

	return []workflowOption{withContextHead(render.SummaryHead)}
}

func renderWorkflow(w workflowstore.Workflow, mark *string, opts ...workflowOption) workflow {
	options := workflowOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	sharedContext, contextTruncated := w.Context, false
	if options.contextHead > 0 {
		sharedContext, contextTruncated = xstr.Head(w.Context, options.contextHead)
	}

	return workflow{
		Workflow: &workflowv1.Workflow{
			Id:               w.ID,
			Name:             w.Name,
			Context:          sharedContext,
			Harness:          w.Harness,
			Cwd:              w.Cwd,
			Flags:            w.Flags,
			Mark:             mark,
			ArchivedAt:       dates.StampPtr(w.ArchivedAt),
			ContextTruncated: contextTruncated,
			Worktree: &workflowv1.WorktreeConfig{
				Enabled:      w.Worktree.Enabled,
				BaseBranch:   w.Worktree.BaseBranch,
				Dir:          w.Worktree.Dir,
				CopyPaths:    w.Worktree.CopyPaths,
				SymlinkPaths: w.Worktree.SymlinkPaths,
				DiffExclude:  w.Worktree.DiffExclude,
			},
			ArchiveOnPrClosed: w.ArchiveOnPRClosed,
			Hooks: xslice.Map(w.Hooks, func(hook workflowstore.Hook) *workflowv1.WorkflowHook {
				return &workflowv1.WorkflowHook{Event: hook.Event, Script: hook.Script}
			}),
		},
		cursor: workflowstore.Cursor{Name: w.Name, ID: w.ID},
	}
}
