package ui

import (
	"fmt"

	"komrad/internal/ui/form"
	"komrad/internal/ui/panel"
)

// deleteChosenConfirm asks what goes when the tasks of chosen agents are deleted.
func deleteChosenConfirm(work chosenWork, offersWorktree bool) form.DeleteConfirm {
	return form.DeleteConfirm{
		Title:           "Delete " + panel.DescribeTasks(work.wholeTasks) + "?",
		Dirs:            panel.TaskDirs(work.wholeTasks),
		StartsConfirmed: true,
		Values:          form.NewDeleteValues(offersWorktree),
	}
}

// deleteWorkflowConfirm asks what goes when an archived workflow is deleted.
func deleteWorkflowConfirm(workflow panel.Workflow, offersWorktree bool) form.DeleteConfirm {
	return form.DeleteConfirm{
		Title:           fmt.Sprintf("Delete workflow %q?", workflow.Name),
		StartsConfirmed: true,
		Values:          form.NewDeleteValues(offersWorktree),
	}
}
