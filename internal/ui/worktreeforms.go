package ui

import (
	"fmt"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint/label"

	"charm.land/huh/v2"
)

// deleteWorktreesForm asks before the remover takes the directories no task answers for.
func deleteWorktreesForm(deletion worktreeDeletion, keys config.FixedKeys) (*huh.Form, *bool) {
	count := deletion.count()
	subject := label.Count(count, "worktree", "worktrees")
	if count == 1 {
		subject = "the worktree of " + worktreeRow{item: deletion.unowned[0]}.name()
	}
	title := fmt.Sprintf("Delete %s?", subject)

	var lines []string
	switch {
	case count == 1:
		lines = append(lines, "The directory nothing answers for is removed on its own, and no branch is touched.")
	case count > 1:
		lines = append(lines, fmt.Sprintf("%d directories nothing answers for are removed on their own, and no branch is touched.", count))
	}
	lines = append(lines, "Uncommitted changes go with the directories.")

	return confirmFormStartingOn(false, title, strings.Join(lines, " "), "Delete", keys)
}

// deleteMixedForm asks before a choice of task checkouts and directories no task answers for
// goes, the checkouts with their tasks.
func deleteMixedForm(deletion worktreeDeletion, agentCount int, keys config.FixedKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Permanently delete %s?", label.Count(deletion.chosen(), "worktree", "worktrees"))

	lines := []string{
		"A task's worktree goes with its task, for good, along with its agents, documents and reviews.",
		"The directories no task answers for are removed on their own, and no branch is touched.",
	}
	if agentCount > 0 {
		lines = append(lines, label.Count(agentCount, "live agent is", "live agents are")+" archived first.")
	}
	lines = append(lines, "Uncommitted changes go with the directories.")

	return confirmFormStartingOn(false, title, strings.Join(lines, " "), "Delete", keys)
}
