package workflow

import (
	workflowstore "claude-agents/internal/store/workflow"
	workflowv1 "claude-agents/proto/gen/workflow/v1"
)

// Every resource the service lists is paged the same way. The handler renders
// stored rows into one of the types below, each pairing the proto message the
// caller sees with the cursor locating it, and hands the slice to a paginator. The
// cursor lives beside the proto rather than inside it, since it is the server's
// bookkeeping rather than part of the resource.

// workflow is a rendered workflow, the proto sent to the caller plus the cursor
// that finds it again in the listing.
type workflow struct {
	*workflowv1.Workflow
	cursor workflowstore.Cursor
}

// Cursor returns the keyset cursor locating this workflow.
func (w workflow) Cursor() (workflowstore.Cursor, error) {
	return w.cursor, nil
}

// renderWorkflow renders a stored workflow into a workflow resource.
func renderWorkflow(w workflowstore.Workflow) workflow {
	return workflow{
		Workflow: &workflowv1.Workflow{
			Id:      w.ID,
			Name:    w.Name,
			Context: w.Context,
			Harness: w.Harness,
			Cwd:     w.Cwd,
			Flags:   w.Flags,
		},
		cursor: workflowstore.Cursor{ID: w.ID},
	}
}
