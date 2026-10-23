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
func taskDetailsTags(inspections map[string]panel.WorktreeInspection, task panel.Task) []string {
	worktree, _ := taskWorktree(inspections, task.Cwd)

	return append(buildWorktreeStateTags(worktree), buildDiffStatTag(task.DiffStat))
}

// buildWorktreeStateTags is what is uncommitted, unpushed and unpulled in the directory.
func buildWorktreeStateTags(read panel.WorktreeInspection) []string {
	if !read.IsRead() || !read.Item.Readable {
		return nil
	}

	warning := paint.Panel.Style(paint.ToneWarning)
	var parts []string
	if n := len(read.Modified); n > 0 {
		parts = append(parts, warning.Render(label.Count(n, "modified", "modified")))
	}
	if n := len(read.Untracked); n > 0 {
		parts = append(parts, warning.Render(label.Count(n, "untracked", "untracked")))
	}
	switch {
	case read.Item.Branch == "":
	case !read.Pushed && (read.BaseRef == "" || read.Ahead > 0):
		parts = append(parts, warning.Render("not pushed"))
	case read.Pushed && read.Unpushed > 0 && (read.Ahead > 0 || !read.UnpulledLanded):
		parts = append(parts, warning.Render(fmt.Sprintf("%s%d unpushed", glyph.Up, read.Unpushed)))
	}
	if read.Pushed && read.Unpulled > 0 && !read.UnpulledLanded {
		parts = append(parts, warning.Render(fmt.Sprintf("%s%d unpulled", glyph.Down, read.Unpulled)))
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
		return paint.Panel.Style(paint.ToneMuted).Render("clean")
	}

	return paint.Panel.Style(paint.ToneSuccess).Render(fmt.Sprintf("+%d", stat.GetAdded())) + " " + paint.Panel.Style(paint.ToneDanger).Render(fmt.Sprintf("−%d", stat.GetRemoved()))
}
