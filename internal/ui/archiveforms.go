package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"

	"charm.land/huh/v2"
)

// deleteChosenConfirm asks what goes when the tasks of chosen agents are deleted.
func deleteChosenConfirm(work chosenWork, offersWorktree bool) worktreeConfirm {
	return worktreeConfirm{
		kind:            formArchiveDelete,
		title:           "Delete " + describeTasks(work.wholeTasks) + "?",
		dirs:            taskDirs(work.wholeTasks),
		startsConfirmed: true,
		values:          newDeleteFormValues(offersWorktree),
	}
}

// deleteWorkflowConfirm asks what goes when an archived workflow is deleted.
func deleteWorkflowConfirm(workflow panel.Workflow) worktreeConfirm {
	return worktreeConfirm{
		kind:            formArchiveDelete,
		title:           fmt.Sprintf("Delete workflow %q?", workflow.Name),
		startsConfirmed: true,
		values:          newDeleteFormValues(workflow.Worktree.Enabled),
	}
}

func deleteScheduleForm(name string, keys config.FixedKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Permanently delete schedule %q?", name),
		"Komrad deletes its run history too and keeps the agents it launched.",
		"Delete",
		keys,
	)
}

// deleteTasksConfirm asks what goes when the tasks are deleted.
func deleteTasksConfirm(tasks []panel.Task, agentCount int, offersWorktree bool) worktreeConfirm {
	warning := ""
	if agentCount > 0 {
		warning = fmt.Sprintf("Komrad archives %s first.", label.Count(agentCount, "live agent", "live agents"))
	}

	return worktreeConfirm{
		kind:            formArchiveDelete,
		title:           "Delete " + describeTasks(tasks) + "?",
		warning:         warning,
		dirs:            taskDirs(tasks),
		startsConfirmed: true,
		values:          newDeleteFormValues(offersWorktree),
	}
}
