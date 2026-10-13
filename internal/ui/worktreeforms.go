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
		lines = append(lines, "Komrad deletes the directory, which no agent uses, and leaves its branch alone.")
	case count > 1:
		lines = append(lines, fmt.Sprintf("Komrad deletes %d directories that no agent uses and leaves every branch alone.", count))
	}
	lines = append(lines, "You lose any uncommitted changes there.")

	return confirmFormStartingOn(confirmFormStartingOnParams{
		confirmed:   false,
		title:       title,
		description: strings.Join(lines, " "),
		affirmative: "Delete",
		keys:        keys,
	})
}

// deleteMixedForm asks before task checkouts and other directories are deleted together.
func deleteMixedForm(deletion worktreeDeletion, agentCount int, keys config.FixedKeys) (*huh.Form, *bool) {
	title := fmt.Sprintf("Permanently delete %s?", label.Count(deletion.chosen(), "worktree", "worktrees"))

	lines := []string{
		"Komrad deletes each task's worktree for good, along with the task's agents, documents and reviews.",
		"It deletes the directories no agent uses and leaves every branch alone.",
	}
	if agentCount > 0 {
		lines = append(lines, "It archives "+label.Count(agentCount, "live agent", "live agents")+" first.")
	}
	lines = append(lines, "You lose any uncommitted changes there.")

	return confirmFormStartingOn(confirmFormStartingOnParams{
		confirmed:   false,
		title:       title,
		description: strings.Join(lines, " "),
		affirmative: "Delete",
		keys:        keys,
	})
}
