package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"

	"charm.land/huh/v2"
)

func deleteAgentsForm(agents []panel.Agent, reviewers int, keys config.FixedKeys) (*huh.Form, *bool) {
	title := "Permanently delete " + promptSubject(agents, reviewers) + "?"

	return confirmForm(title, transcriptWarning(agents, reviewers), "Delete", keys)
}

// deleteChosenForm asks before chosen agents are deleted, naming any task that goes whole with them.
func deleteChosenForm(work chosenWork, reviewers int, keys config.FixedKeys) (*huh.Form, *bool) {
	if len(work.wholeTasks) == 0 {
		return deleteAgentsForm(work.chosen, reviewers, keys)
	}

	title := "Permanently delete " + promptSubject(work.chosen, reviewers) + "?"

	tasksGoing := fmt.Sprintf("%d tasks go too, with the documents, reviews and worktrees on them.", len(work.wholeTasks))
	if len(work.wholeTasks) == 1 {
		tasksGoing = fmt.Sprintf("Task %q goes too, with the documents, reviews and worktree on it.", work.wholeTasks[0].Slug)
	}

	return confirmForm(title, transcriptWarning(work.chosen, reviewers)+" "+tasksGoing, "Delete", keys)
}

// transcriptWarning says the transcripts of the agents being deleted go with them.
func transcriptWarning(agents []panel.Agent, reviewers int) string {
	if len(agents) == 1 && reviewers == 0 {
		return "Its transcript goes with it and search will not find the conversation again."
	}

	return "Their transcripts go with them and search will not find the conversations again."
}

func deleteWorkflowForm(workflow panel.Workflow, agentCount int, keys config.FixedKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Permanently delete workflow %q?", workflow.Name)

	warning := "It holds no agents."
	if agentCount == 1 {
		warning = "The one agent still archived inside it goes with it, its transcript included."
	} else if agentCount > 1 {
		warning = fmt.Sprintf("The %d agents still archived inside it go with it, their transcripts included.", agentCount)
	}
	warning += " Any archived schedule that launched into it goes too."

	return confirmForm(title, warning, "Delete", keys)
}

func deleteScheduleForm(name string, keys config.FixedKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Permanently delete schedule %q?", name),
		"Its run history goes with it. The agents it launched are kept.",
		"Delete",
		keys,
	)
}

func deleteTasksForm(tasks []panel.Task, agentCount int, keys config.FixedKeys) (*huh.Form, *bool) {
	title := "Permanently delete " + describeTasks(tasks) + "?"

	warning := "Their agents, documents, reviews and worktrees go with them."
	if len(tasks) == 1 {
		warning = "Its agents, documents, reviews and worktree go with it."
	}
	if agentCount > 0 {
		warning = fmt.Sprintf("%s %s archived first.", warning, label.Count(agentCount, "live agent is", "live agents are"))
	}

	return confirmForm(title, warning, "Delete", keys)
}
