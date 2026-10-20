package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"

	"charm.land/huh/v2"
)

// deleteChosenConfirm asks what goes when the tasks of chosen agents are deleted.
func deleteChosenConfirm(work chosenWork, reviewers int, offersWorktree bool) worktreeConfirm {
	return worktreeConfirm{
		kind:            formArchiveDelete,
		title:           "Delete " + describeTasks(work.wholeTasks) + "?",
		warning:         "With records checked, Komrad also deletes " + promptSubject(work.chosen, reviewers) + ".",
		dirs:            taskDirs(work.wholeTasks),
		startsConfirmed: true,
		values:          newDeleteFormValues(offersWorktree),
	}
}

// deleteWorkflowConfirm asks what goes when an archived workflow is deleted.
func deleteWorkflowConfirm(workflow panel.Workflow, agentCount int) worktreeConfirm {
	agentsInIt := ""
	switch {
	case agentCount == 1:
		agentsInIt = ", the one archived agent in it"
	case agentCount > 1:
		agentsInIt = fmt.Sprintf(", the %d archived agents in it", agentCount)
	}

	warning := fmt.Sprintf("With records checked, Komrad also deletes the workflow%s and any archived schedule that launches into it.", agentsInIt)

	return worktreeConfirm{
		kind:            formArchiveDelete,
		title:           fmt.Sprintf("Delete workflow %q?", workflow.Name),
		warning:         warning,
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
