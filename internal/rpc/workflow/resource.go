package workflow

import (
	"komrade/internal/rpc/render"

	workflowstore "komrade/internal/store/workflow"
	viewv1 "komrade/proto/gen/view/v1"
	workflowv1 "komrade/proto/gen/workflow/v1"
)

type workflow struct {
	*workflowv1.Workflow
	cursor workflowstore.Cursor
}

// Cursor returns the keyset cursor locating this workflow.
func (w workflow) Cursor() (workflowstore.Cursor, error) {
	return w.cursor, nil
}

// workflowOptions is how much of a workflow is drawn.
type workflowOptions struct {
	contextHead int
}

type workflowOption = render.LoadOption[workflowOptions]

// withContextHead draws the opening of the context rather than the whole of it.
func withContextHead(limit int) workflowOption {
	return func(o *workflowOptions) { o.contextHead = limit }
}

// workflowViewOptions turns the view a request asked for into rendering options.
func workflowViewOptions(view viewv1.View) []workflowOption {
	if view == viewv1.View_VIEW_FULL {
		return nil
	}

	return []workflowOption{withContextHead(render.SummaryHead)}
}

func renderWorkflow(w workflowstore.Workflow, opts ...workflowOption) workflow {
	options := workflowOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	return workflow{
		Workflow: render.Workflow(w, render.WorkflowOptions{ContextHead: options.contextHead}),
		cursor:   workflowstore.Cursor{Name: w.Name, ID: w.ID},
	}
}
