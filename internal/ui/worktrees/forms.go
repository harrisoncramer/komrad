package worktrees

import (
	"fmt"
	"slices"
	"strings"

	"komrad/internal/ui/form"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
)

// deleteWorktreesConfirm asks before the remover takes the directories no task answers for.
func deleteWorktreesConfirm(deletion deletion) form.DeleteConfirm {
	count := deletion.count()
	subject := label.Count(count, "worktree", "worktrees")
	warning := fmt.Sprintf("Komrad deletes %d directories that no agent uses and leaves every branch alone.", count)
	if count == 1 {
		subject = "the worktree of " + row{item: deletion.unowned[0]}.name()
		warning = "Komrad deletes the directory, which no agent uses, and leaves its branch alone."
	}

	return form.DeleteConfirm{
		Title:   fmt.Sprintf("Delete %s?", subject),
		Warning: warning,
		Dirs:    deletion.dirs(),
	}
}

// deleteMixedConfirm asks before task checkouts and other directories are deleted together.
func deleteMixedConfirm(deletion deletion, agentCount int) form.DeleteConfirm {
	lines := []string{
		"Komrad deletes the directories no agent uses and leaves every branch alone.",
	}
	if agentCount > 0 {
		lines = append(lines, "It archives "+label.Count(agentCount, "live agent", "live agents")+" first.")
	}

	return form.DeleteConfirm{
		Title:   fmt.Sprintf("Delete %s?", label.Count(deletion.chosen(), "worktree", "worktrees")),
		Warning: strings.Join(lines, " "),
		Dirs:    slices.Concat(deletion.dirs(), panel.TaskDirs(deletion.tasks)),
		Values:  form.NewDeleteValues(true),
	}
}
