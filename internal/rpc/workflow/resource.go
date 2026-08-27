package workflow

import (
	"time"

	workflowstore "weave/internal/store/workflow"
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

func renderWorkflow(w workflowstore.Workflow) workflow {
	var archived *string
	if w.ArchivedAt != nil {
		formatted := w.ArchivedAt.Format(time.RFC3339)
		archived = &formatted
	}

	return workflow{
		Workflow: &workflowv1.Workflow{
			Id:            w.ID,
			Name:          w.Name,
			Context:       w.Context,
			Harness:       w.Harness,
			Cwd:           w.Cwd,
			Flags:         w.Flags,
			SetupAction:   w.SetupAction,
			CleanupAction: w.CleanupAction,
			ArchivedAt:    archived,
			Worktree: &workflowv1.WorktreeConfig{
				Enabled:      w.Worktree.Enabled,
				BaseBranch:   w.Worktree.BaseBranch,
				CopyPaths:    w.Worktree.CopyPaths,
				SymlinkPaths: w.Worktree.SymlinkPaths,
				DiffExclude:  w.Worktree.DiffExclude,
			},
		},
		cursor: workflowstore.Cursor{ID: w.ID},
	}
}
