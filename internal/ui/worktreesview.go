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
	"komrad/internal/view"
	"komrad/lib/xsha"
	"komrad/lib/xslice"
)

func (w *worktreesScreen) view(m *model) string {
	return m.columnListView(w.listParams(m))
}

func (w *worktreesScreen) snapshot(m *model, pane list.Pane) view.Snapshot {
	rows := w.rows(m)

	return worktreesSnapshot(worktreesSnapshotParams{
		rows:     rows,
		items:    w.shown(),
		selected: w.Selected,
		focused:  list.KeyAt(rows, w.Row),
		live:     m.listing.Agents,
		flat:     w.sort.flat,
		caret:    pane.Caret,
		matcher:  m.filter.pattern,
		pane:     pane.Width,
		now:      m.clock.Now(),
	})
}

func (w *worktreesScreen) details(m *model) details {
	row, ok := list.At(w.rows(m), w.Row)
	if !ok {
		return details{}
	}

	if row.header {
		return details{
			id:     row.Key(),
			fields: []recordField{{label: "repository", value: row.repo}, {label: "worktrees", value: fmt.Sprintf("%d", w.countUnder(row.repo))}},
		}
	}

	return details{id: row.Key(), fields: worktreeFields(m, row.item, m.clock.Now())}
}

func worktreeFields(m *model, item panel.Worktree, now time.Time) []recordField {
	fields := []recordField{
		{label: "name", value: worktreeRow{item: item}.name()},
		{label: "directory", value: item.Dir},
		{label: "size", value: sizeField(item, now)},
		{label: "last active", value: lastActiveField(item, now)},
	}

	if !item.Readable {
		fields = append(fields, recordField{label: "repository", value: label.OrDash(item.RepoDir)}, worktreeTaskField(item))

		return appendOccupantsField(fields, "agents", item.Occupants)
	}

	fields = append(fields,
		recordField{label: "branch", value: label.OrDash(item.Branch)},
		recordField{label: "repository", value: label.OrDash(label.Path(item.RepoDir, homeDir()))},
	)
	read := m.inspections[item.Dir]
	if read.Answered && read.Err != nil {
		fields = append(fields, recordField{label: "worktree", value: "could not read: " + errui.GetReason(read.Err)})
	}
	if read.IsRead() {
		fields = append(fields,
			buildBaseField(read),
			recordField{label: "changes", value: changesField(read)},
		)
	}

	fields = append(fields, worktreeTaskField(item), recordField{label: "workflow", value: worktreeWorkflowField(m.listing, item)})

	return appendOccupantsField(fields, "agents", item.Occupants)
}

// worktreeTaskField names the task a directory is the checkout of.
func worktreeTaskField(item panel.Worktree) recordField {
	if item.Task == nil {
		return recordField{label: "task", value: "none"}
	}

	return recordField{label: "task", value: item.Task.Slug}
}

// appendOccupantsField names the agents under the label when there are any.
func appendOccupantsField(fields []recordField, name string, occupants []panel.WorktreeAgent) []recordField {
	if len(occupants) == 0 {
		return fields
	}

	return append(fields, recordField{label: name, value: strings.Join(xslice.Map(occupants, func(occupant panel.WorktreeAgent) string { return occupant.Name }), ", ")})
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
func buildBaseField(read panel.WorktreeInspection) recordField {
	if read.BaseRef == "" {
		return recordField{label: "base", value: label.OrDash(read.BaseError)}
	}
	if read.Ahead == 0 && read.Behind == 0 {
		return recordField{
			label: "base",
			value: read.BaseRef,
			tail:  view.Cell{Text: "even", Tone: paint.ToneMuted},
		}
	}

	ahead := fmt.Sprintf("%s%d", glyph.Up, read.Ahead)
	behind := fmt.Sprintf("%s%d", glyph.Down, read.Behind)

	return recordField{
		label: "base",
		value: read.BaseRef,
		tail: view.Cell{Parts: []view.Cell{
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
func (w *worktreesScreen) doc(m *model) string {
	return w.gitDoc(m)
}

func (w *worktreesScreen) gitDoc(m *model) string {
	row, ok := list.At(w.rows(m), w.Row)
	switch {
	case !ok:
		return renderDocument("Worktrees", "_no worktrees_", "", "")
	case row.header:
		return renderDocument(label.Path(row.repo, homeDir()), fmt.Sprintf("_%s_", label.Count(w.countUnder(row.repo), "worktree", "worktrees")), "", "")
	}

	read, ok := m.inspections[row.item.Dir]
	switch {
	case !ok || !read.Answered:
		return renderDocument(row.name(), "_"+row.item.Dir+"_", "_reading the directory…_", "")
	case read.Err != nil && !read.IsRead():
		return renderDocument(row.name(), "_"+row.item.Dir+"_", "could not read: "+errui.GetReason(read.Err), "")
	case read.Err != nil:
		return renderDocument(row.name(), "_"+row.item.Dir+"_", "_could not read: "+errui.GetReason(read.Err)+"_\n\n"+worktreeProse(row.item, read, m.clock.Now()), "")
	}

	return renderDocument(row.name(), "_"+row.item.Dir+"_", worktreeProse(row.item, read, m.clock.Now()), "")
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
