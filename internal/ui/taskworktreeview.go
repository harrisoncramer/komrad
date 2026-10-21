package ui

import (
	"fmt"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	taskv1 "komrad/proto/gen/task/v1"
)

// taskDetailsTags is what the record's rule carries for a task.
func taskDetailsTags(m *model, task panel.Task) []string {
	worktree, _ := taskWorktree(m.inspections, task.Cwd)

	return append(buildWorktreeStateTags(worktree), buildDiffStatTag(task.DiffStat))
}

// buildWorktreeStateTags is what is uncommitted, unpushed and unpulled in the directory.
func buildWorktreeStateTags(read worktreeInspection) []string {
	if !read.isRead() || !read.item.readable {
		return nil
	}

	warning := paint.Panel.Style(paint.ToneWarning)
	var parts []string
	if n := len(read.modified); n > 0 {
		parts = append(parts, warning.Render(label.Count(n, "modified", "modified")))
	}
	if n := len(read.untracked); n > 0 {
		parts = append(parts, warning.Render(label.Count(n, "untracked", "untracked")))
	}
	switch {
	case read.item.branch == "":
	case !read.pushed && (read.baseRef == "" || read.ahead > 0):
		parts = append(parts, warning.Render("not pushed"))
	case read.pushed && read.unpushed > 0 && (read.ahead > 0 || !read.unpulledLanded):
		parts = append(parts, warning.Render(fmt.Sprintf("%s%d unpushed", glyph.Up, read.unpushed)))
	}
	if read.pushed && read.unpulled > 0 && !read.unpulledLanded {
		parts = append(parts, warning.Render(fmt.Sprintf("%s%d unpulled", glyph.Down, read.unpulled)))
	}

	return parts
}

// buildDiffStatTag is the lines the branch adds and removes since its base, and nothing where no
// base could be measured against.
func buildDiffStatTag(stat *taskv1.DiffStat) string {
	if stat == nil {
		return ""
	}
	if stat.GetAdded() == 0 && stat.GetRemoved() == 0 {
		return dimStyle().Render("clean")
	}

	return paint.Panel.Style(paint.ToneSuccess).Render(fmt.Sprintf("+%d", stat.GetAdded())) + " " + paint.Panel.Style(paint.ToneDanger).Render(fmt.Sprintf("−%d", stat.GetRemoved()))
}
