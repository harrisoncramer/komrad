package ui

import (
	"fmt"

	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
)

// deleteChosenConfirm asks what goes when the tasks of chosen agents are deleted.
func deleteChosenConfirm(work chosenWork, offersWorktree bool) worktreeConfirm {
	return worktreeConfirm{
		title:           "Delete " + panel.DescribeTasks(work.wholeTasks) + "?",
		dirs:            taskDirs(work.wholeTasks),
		startsConfirmed: true,
		values:          newDeleteFormValues(offersWorktree),
	}
}

// deleteWorkflowConfirm asks what goes when an archived workflow is deleted.
func deleteWorkflowConfirm(workflow panel.Workflow, offersWorktree bool) worktreeConfirm {
	return worktreeConfirm{
		title:           fmt.Sprintf("Delete workflow %q?", workflow.Name),
		startsConfirmed: true,
		values:          newDeleteFormValues(offersWorktree),
	}
}

// deleteTasksConfirm asks what goes when the tasks are deleted.
func deleteTasksConfirm(tasks []panel.Task, agentCount int, offersWorktree bool) worktreeConfirm {
	warning := ""
	if agentCount > 0 {
		warning = fmt.Sprintf("Komrad archives %s first.", label.Count(agentCount, "live agent", "live agents"))
	}

	return worktreeConfirm{
		title:           "Delete " + panel.DescribeTasks(tasks) + "?",
		warning:         warning,
		dirs:            taskDirs(tasks),
		startsConfirmed: true,
		values:          newDeleteFormValues(offersWorktree),
	}
}
