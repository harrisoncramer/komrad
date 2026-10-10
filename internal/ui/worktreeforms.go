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
