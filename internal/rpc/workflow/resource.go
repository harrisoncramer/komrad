package workflow

import (
	"komrad/internal/rpc/render"

	workflowstore "komrad/internal/store/workflow"
	viewv1 "komrad/proto/gen/view/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
)

type workflow struct {
	*workflowv1.Workflow
	cursor workflowstore.Cursor
}

// Cursor returns the keyset cursor locating this workflow.
func (w workflow) Cursor() (workflowstore.Cursor, error) {
	return w.cursor, nil
}

// workflowViewOptions turns the view a request asked for into rendering options.
func workflowViewOptions(view viewv1.View) render.WorkflowOptions {
	if view == viewv1.View_VIEW_FULL {
		return render.WorkflowOptions{}
	}

	return render.WorkflowOptions{ContextHead: render.SummaryHead}
}

func renderWorkflow(w workflowstore.Workflow, options render.WorkflowOptions) workflow {
	return workflow{
		Workflow: render.Workflow(w, options),
		cursor:   workflowstore.Cursor{Name: w.Name, ID: w.ID},
	}
}
