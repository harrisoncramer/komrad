package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"

	"charm.land/huh/v2"
)

// deleteChosenConfirm asks what goes when chosen agents are deleted, naming the tasks deleted with them.
func deleteChosenConfirm(work chosenWork, reviewers int, offersWorktree bool) worktreeConfirm {
	confirm := worktreeConfirm{
		kind:            formArchiveDelete,
		title:           "Delete " + promptSubject(work.chosen, reviewers) + "?",
		dirs:            taskDirs(work.wholeTasks),
		startsConfirmed: true,
		boxes:           newDeleteBoxes(offersWorktree),
	}

	switch {
	case len(work.wholeTasks) == 1:
		confirm.warning = fmt.Sprintf("Komrad deletes them as part of task %q.", work.wholeTasks[0].Slug)
	case len(work.wholeTasks) > 1:
		confirm.warning = fmt.Sprintf("Komrad deletes them as part of the %d tasks they work on.", len(work.wholeTasks))
	}

	return confirm
}

// deleteWorkflowConfirm asks what goes when an archived workflow is deleted.
func deleteWorkflowConfirm(workflow panel.Workflow, agentCount int) worktreeConfirm {
	agents := ""
	switch {
	case agentCount == 1:
		agents = ", the one archived agent in it"
	case agentCount > 1:
		agents = fmt.Sprintf(", the %d archived agents in it", agentCount)
	}
	warning := fmt.Sprintf("With records ticked, Komrad also deletes the workflow%s and any archived schedule that launches into it.", agents)

	return worktreeConfirm{
		kind:            formArchiveDelete,
		title:           fmt.Sprintf("Delete workflow %q?", workflow.Name),
		warning:         warning,
		startsConfirmed: true,
		boxes:           newDeleteBoxes(workflow.Worktree.Enabled),
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
		boxes:           newDeleteBoxes(offersWorktree),
	}
}
