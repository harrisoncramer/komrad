package ui

import (
	"fmt"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	"komrad/lib/xsha"
	"komrad/lib/xslice"
)

func (w *worktreesScreen) snapshot(in screen.Inputs, pane list.Pane) view.Snapshot {
	rows := w.rows(in)

	return worktreesSnapshot(worktreesSnapshotParams{
		rows:     rows,
		items:    w.shown(),
		selected: w.Selected,
		focused:  list.KeyAt(rows, w.Row),
		live:     in.Listing.Agents,
		flat:     w.sort.Flat,
		caret:    pane.Caret,
		matcher:  in.Room.Pattern.Regexp,
		pane:     pane.Width,
		now:      in.Room.Now,
	})
}

func (w *worktreesScreen) Details(in screen.Inputs) record.Details {
	row, ok := list.At(w.rows(in), w.Row)
	if !ok {
		return record.Details{}
	}

	if row.header {
		return record.Details{
			ID:     row.Key(),
			Fields: []record.Field{{Label: "repository", Value: row.repo}, {Label: "worktrees", Value: fmt.Sprintf("%d", w.countUnder(row.repo))}},
		}
	}

	return record.Details{ID: row.Key(), Fields: worktreeFields(in, row.item)}
}

func worktreeFields(in screen.Inputs, item panel.Worktree) []record.Field {
	fields := []record.Field{
		{Label: "name", Value: worktreeRow{item: item}.name()},
		{Label: "directory", Value: item.Dir},
		{Label: "size", Value: sizeField(item, in.Room.Now)},
		{Label: "last active", Value: lastActiveField(item, in.Room.Now)},
	}

	if !item.Readable {
		fields = append(fields, record.Field{Label: "repository", Value: label.OrDash(item.RepoDir)}, worktreeTaskField(item))

		return appendOccupantsField(fields, "agents", item.Occupants)
	}

	fields = append(fields,
		record.Field{Label: "branch", Value: label.OrDash(item.Branch)},
		record.Field{Label: "repository", Value: label.OrDash(label.Path(item.RepoDir, homeDir()))},
	)
	read := in.Inspections[item.Dir]
	if read.Answered && read.Err != nil {
		fields = append(fields, record.Field{Label: "worktree", Value: "could not read: " + errui.GetReason(read.Err)})
	}
	if read.IsRead() {
		fields = append(fields,
			buildBaseField(read),
			record.Field{Label: "changes", Value: changesField(read)},
		)
	}

	fields = append(fields, worktreeTaskField(item), record.Field{Label: "workflow", Value: worktreeWorkflowField(in.Listing, item)})

	return appendOccupantsField(fields, "agents", item.Occupants)
}

// worktreeTaskField names the task a directory is the checkout of.
func worktreeTaskField(item panel.Worktree) record.Field {
	if item.Task == nil {
		return record.Field{Label: "task", Value: "none"}
	}

	return record.Field{Label: "task", Value: item.Task.Slug}
}

// appendOccupantsField names the agents under the label when there are any.
func appendOccupantsField(fields []record.Field, name string, occupants []panel.WorktreeAgent) []record.Field {
	if len(occupants) == 0 {
		return fields
	}

	return append(fields, record.Field{Label: name, Value: strings.Join(xslice.Map(occupants, func(occupant panel.WorktreeAgent) string { return occupant.Name }), ", ")})
}

// sizeField is what the directory takes and when that was measured, or why it could not be.
func sizeField(item panel.Worktree, now time.Time) string {
	switch {
	case item.Size == nil:
		return label.Dash
	case item.Size.Err != "":
		return label.Dash + ", could not measure: " + item.Size.Err
	}

	return fmt.Sprintf("%s, measured %s ago", label.Bytes(item.Size.Bytes), label.AgeAt(item.Size.MeasuredAt, now))
}

// lastActiveField is how long ago the directory was last touched.
func lastActiveField(item panel.Worktree, now time.Time) string {
	if item.LastActiveAt.IsZero() {
		return label.Dash
	}

	field := label.AgeAt(item.LastActiveAt, now) + " ago"
	if item.Task != nil && item.Task.ArchivedAt != nil {
		field += ", archived"
	}

	return field
}

// buildBaseField spells where the branch stands against its base, or why git could not name one.
func buildBaseField(read panel.WorktreeInspection) record.Field {
	if read.BaseRef == "" {
		return record.Field{Label: "base", Value: label.OrDash(read.BaseError)}
	}
	if read.Ahead == 0 && read.Behind == 0 {
		return record.Field{
			Label: "base",
			Value: read.BaseRef,
			Tail:  view.Cell{Text: "even", Tone: paint.ToneMuted},
		}
	}

	ahead := fmt.Sprintf("%s%d", glyph.Up, read.Ahead)
	behind := fmt.Sprintf("%s%d", glyph.Down, read.Behind)

	return record.Field{
		Label: "base",
		Value: read.BaseRef,
		Tail: view.Cell{Parts: []view.Cell{
			{Text: ahead, Tone: countTone(read.Ahead, paint.ToneSuccess)},
			{Text: " "},
			{Text: behind, Tone: countTone(read.Behind, paint.ToneWarning)},
		}},
	}
}

func countTone(count int, tone paint.Tone) paint.Tone {
	if count == 0 {
		return paint.ToneMuted
	}

	return tone
}

func changesField(read panel.WorktreeInspection) string {
	var parts []string
	if n := len(read.Modified); n > 0 {
		parts = append(parts, label.Count(n, "modified", "modified"))
	}
	if n := len(read.Untracked); n > 0 {
		parts = append(parts, label.Count(n, "untracked", "untracked"))
	}
	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}

func worktreeWorkflowField(l panel.Listing, item panel.Worktree) string {
	if item.Task == nil {
		return label.Dash
	}

	return l.WorkflowName(item.Task.WorkflowID)
}

// doc is the markdown rendered beside the list for the row the cursor is on.
func (w *worktreesScreen) doc(in screen.Inputs) string {
	return w.gitDoc(in)
}

func (w *worktreesScreen) gitDoc(in screen.Inputs) string {
	row, ok := list.At(w.rows(in), w.Row)
	switch {
	case !ok:
		return paint.DocumentMarkdown("Worktrees", "_no worktrees_", "", "")
	case row.header:
		return paint.DocumentMarkdown(label.Path(row.repo, homeDir()), fmt.Sprintf("_%s_", label.Count(w.countUnder(row.repo), "worktree", "worktrees")), "", "")
	}

	read, ok := in.Inspections[row.item.Dir]
	switch {
	case !ok || !read.Answered:
		return paint.DocumentMarkdown(row.name(), "_"+row.item.Dir+"_", "_reading the directory…_", "")
	case read.Err != nil && !read.IsRead():
		return paint.DocumentMarkdown(row.name(), "_"+row.item.Dir+"_", "could not read: "+errui.GetReason(read.Err), "")
	case read.Err != nil:
		return paint.DocumentMarkdown(row.name(), "_"+row.item.Dir+"_", "_could not read: "+errui.GetReason(read.Err)+"_\n\n"+worktreeProse(row.item, read, in.Room.Now), "")
	}

	return paint.DocumentMarkdown(row.name(), "_"+row.item.Dir+"_", worktreeProse(row.item, read, in.Room.Now), "")
}

// worktreeProse is the three blocks the document renders, each fenced.
func worktreeProse(item panel.Worktree, read panel.WorktreeInspection, now time.Time) string {
	var b strings.Builder

	b.WriteString("## Contents\n\n")
	fenced(&b, entryLines(read.Entries))

	if !item.Readable {
		b.WriteString("\n_git cannot read this directory; its registration names a repository that is gone_\n")

		return b.String()
	}

	b.WriteString("\n## Uncommitted\n\n")
	if len(read.Modified)+len(read.Untracked) == 0 {
		b.WriteString("_nothing_\n")
	} else {
		fenced(&b, changeLines(read))
	}

	if read.BaseRef != "" {
		fmt.Fprintf(&b, "\n## Commits since %s\n\n", read.BaseRef)
		if len(read.Commits) == 0 {
			b.WriteString("_none_\n")
		} else {
			fenced(&b, commitLines(read.Commits, now))
		}
	}

	return b.String()
}

func fenced(b *strings.Builder, lines []string) {
	b.WriteString("```\n")
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
	b.WriteString("```\n")
}

// entryLines is the biggest entries with their sizes lined up, and one line for the rest.
func entryLines(entries []panel.WorktreeEntry) []string {
	if len(entries) == 0 {
		return []string{"empty"}
	}

	width := 0
	for _, entry := range entries[:min(len(entries), entriesShown)] {
		width = max(width, len(entryName(entry)))
	}

	var lines []string
	var rest int64
	for i, entry := range entries {
		if i >= entriesShown {
			rest += entry.Bytes

			continue
		}

		lines = append(lines, fmt.Sprintf("%-*s  %5s", width, entryName(entry), label.Bytes(entry.Bytes)))
	}
	if len(entries) > entriesShown {
		lines = append(lines, fmt.Sprintf("%-*s  %5s", width, fmt.Sprintf("+ %d more", len(entries)-entriesShown), label.Bytes(rest)))
	}

	return lines
}

func entryName(entry panel.WorktreeEntry) string {
	if entry.Dir {
		return entry.Path + "/"
	}

	return entry.Path
}

func changeLines(read panel.WorktreeInspection) []string {
	lines := xslice.Map(read.Modified, func(path string) string {
		return "M " + path
	})
	lines = append(lines, xslice.Map(read.Untracked, func(path string) string {
		return "? " + path
	})...)

	return lines
}

func commitLines(commits []panel.WorktreeCommit, now time.Time) []string {
	lines := xslice.Map(commits, func(commit panel.WorktreeCommit) string {
		return fmt.Sprintf("%s  %-4s %s", xsha.Short(commit.SHA), label.AgeAt(commit.CommittedAt, now), commit.Subject)
	})

	return lines
}
