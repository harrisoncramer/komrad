package ui

import (
	"fmt"

	"komrad/config"
	"komrad/internal/ui/panel"

	"charm.land/huh/v2"
)

func deleteAgentsForm(agents []panel.Agent, children int, keys config.FormKeys) (*huh.Form, *bool) {
	title := "Permanently delete " + promptSubject(agents, children) + "?"

	warning := "Their transcripts go with them and search will not find the conversations again."
	if len(agents) == 1 && children == 0 {
		warning = "Its transcript goes with it and search will not find the conversation again."
	}

	return confirmForm(title, warning, "Delete", keys)
}

func emptyArchiveForm(count int, keys config.FormKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Permanently delete all %d archived agents?", count)
	warning := "Their transcripts go with them and search will not find the conversations again. Any archived workflow the sweep leaves with no agents in it is deleted too."
	if count == 1 {
		title = "Permanently delete the one archived agent?"
		warning = "Its transcript goes with it and search will not find the conversation again. Any archived workflow the sweep leaves with no agents in it is deleted too."
	}

	return confirmFormStartingOn(false, title, warning, "Empty", keys)
}

func deleteWorkflowForm(workflow panel.Workflow, agentCount int, keys config.FormKeys) (*huh.Form, *bool) {
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
