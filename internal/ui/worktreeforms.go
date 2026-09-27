package ui

import (
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint/label"

	"charm.land/huh/v2"
)

// deleteWorktreesForm asks before a deletion, saying what goes through the agent delete, what the
// remover takes on its own, and where a lone branch stands.
func deleteWorktreesForm(deletion worktreeDeletion, keepsBranch *bool, keys config.FormKeys) (*huh.Form, *bool) {
	count := len(deletion.agents) + len(deletion.unowned)
	subject := label.Count(count, "worktree", "worktrees")
	if count == 1 {
		subject = "the worktree of " + worktreeRow{item: slices.Concat(deletion.agents, deletion.unowned)[0]}.name()
	}
	title := fmt.Sprintf("Delete %s?", subject)

	var lines []string
	switch n := len(deletion.agents); {
	case n == 1:
		lines = append(lines, "The directory goes through the agent delete, which runs the workflow's pre-delete hooks and the cleanup; the agent leaves the archive and its transcript goes with it.")
	case n > 1:
		lines = append(lines, fmt.Sprintf("%d directories go through the agent delete, which runs the workflow's pre-delete hooks and the cleanup; the agents leave the archive and their transcripts go with them.", n))
	}
	switch n := len(deletion.unowned); {
	case n == 1:
		lines = append(lines, "The directory nothing answers for is removed on its own, and no branch is touched.")
	case n > 1:
		lines = append(lines, fmt.Sprintf("%d directories nothing answers for are removed on their own, and no branch is touched.", n))
	}
	switch {
	case keepsBranch == nil:
	case *keepsBranch:
		lines = append(lines, "The branch is kept, since its commits are on no other ref.")
	default:
		lines = append(lines, "The branch goes too; every one of its commits is elsewhere.")
	}
	lines = append(lines, "Uncommitted changes go with the directories.")

	return confirmFormStartingOn(false, title, strings.Join(lines, " "), "Delete", keys)
}
