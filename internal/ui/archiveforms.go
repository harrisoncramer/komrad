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

	tasksGoing := fmt.Sprintf("It also deletes %d tasks and their documents, reviews and worktrees.", len(work.wholeTasks))
	if len(work.wholeTasks) == 1 {
		tasksGoing = fmt.Sprintf("It also deletes task %q and its documents, reviews and worktree.", work.wholeTasks[0].Slug)
	}

	return confirmForm(title, transcriptWarning(work.chosen, reviewers)+" "+tasksGoing, "Delete", keys)
}

// transcriptWarning says the transcripts of the agents being deleted go with them.
func transcriptWarning(agents []panel.Agent, reviewers int) string {
	if len(agents) == 1 && reviewers == 0 {
		return "Komrad deletes its transcript too, so search won't find the conversation again."
	}

	return "Komrad deletes their transcripts too, so search won't find the conversations again."
}

func deleteWorkflowForm(workflow panel.Workflow, agentCount int, keys config.FixedKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Permanently delete workflow %q?", workflow.Name)

	warning := "It holds no agents."
	if agentCount == 1 {
		warning = "Komrad also deletes the one archived agent in it, transcript included."
	} else if agentCount > 1 {
		warning = fmt.Sprintf("Komrad also deletes the %d archived agents in it, transcripts included.", agentCount)
	}
	warning += " Komrad deletes any archived schedule that launches into it too."

	return confirmForm(title, warning, "Delete", keys)
}

func deleteScheduleForm(name string, keys config.FixedKeys) (*huh.Form, *bool) {
	return confirmForm(
		fmt.Sprintf("Permanently delete schedule %q?", name),
		"Komrad deletes its run history too and keeps the agents it launched.",
		"Delete",
		keys,
	)
}

func deleteTasksForm(tasks []panel.Task, agentCount int, keys config.FixedKeys) (*huh.Form, *bool) {
	title := "Permanently delete " + describeTasks(tasks) + "?"

	warning := "Komrad deletes their agents, documents, reviews and worktrees too."
	if len(tasks) == 1 {
		warning = "Komrad deletes its agents, documents, reviews and worktree too."
	}
	if agentCount > 0 {
		warning = fmt.Sprintf("%s It archives %s first.", warning, label.Count(agentCount, "live agent", "live agents"))
	}

	return confirmForm(title, warning, "Delete", keys)
}
