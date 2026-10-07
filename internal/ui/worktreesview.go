package ui

import (
	"fmt"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/git"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/view"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
)

func (w *worktreesScreen) view(m *model) string {
	return m.columnListView(w.listParams(m))
}

func (w *worktreesScreen) snapshot(m *model, pane listPane) view.Snapshot {
	rows := w.rows(m)

	return worktreesSnapshot(worktreesSnapshotParams{
		rows:       rows,
		items:      w.shown(),
		selected:   w.selected,
		focused:    rowKeyAt(rows, w.cursor),
		live:       m.listing.agents,
		flat:       w.sort.flat,
		caret:      pane.caret,
		blinkedOut: pane.blinkedOut,
		matcher:    m.filter.pattern,
		pane:       pane.width,
		now:        time.Now(),
	})
}

func (w *worktreesScreen) details(m *model) details {
	row, ok := rowAt(w.rows(m), w.cursor)
	if !ok {
		return details{}
	}

	if row.header {
		return details{
			id:     row.key(),
			fields: []recordField{{label: "repository", value: row.repo}, {label: "worktrees", value: fmt.Sprintf("%d", w.countUnder(row.repo))}},
		}
	}

	return details{id: row.key(), fields: worktreeFields(m, row.item, time.Now())}
}

func worktreeFields(m *model, item worktreeItem, now time.Time) []recordField {
	fields := []recordField{
		{label: "name", value: worktreeRow{item: item}.name()},
		{label: "directory", value: item.dir},
		{label: "size", value: sizeField(item, now)},
		{label: "last active", value: lastActiveField(item, now)},
	}

	if !item.readable {
		fields = append(fields, recordField{label: "repository", value: label.OrDash(item.repoDir)})
		if len(item.occupants) > 0 {
			fields = append(fields, recordField{label: "also here", value: occupantsField(item)})
		}

		return append(fields, recordField{label: "agent", value: worktreeAgentField(m.listing, item)})
	}

	fields = append(fields,
		recordField{label: "branch", value: label.OrDash(item.branch)},
		recordField{label: "repository", value: label.OrDash(label.Path(item.repoDir, homeDir()))},
	)
	if len(item.occupants) > 0 {
		fields = append(fields, recordField{label: "also here", value: occupantsField(item)})
	}
	read := m.inspections[item.dir]
	if read.answered && read.err != nil {
		fields = append(fields, recordField{label: "worktree", value: "could not read: " + errui.GetReason(read.err)})
	}
	if read.isRead() {
		fields = append(fields,
			buildBaseField(read),
			recordField{label: "changes", value: changesField(read)},
		)
	}

	return append(fields, recordField{label: "agent", value: worktreeAgentField(m.listing, item)}, recordField{label: "workflow", value: worktreeWorkflowField(m.listing, item)})
}

func occupantsField(item worktreeItem) string {
	return strings.Join(xslice.Map(item.occupants, func(occupant worktreeAgent) string { return occupant.name }), ", ")
}

// sizeField is what the directory takes and when that was measured, or why it could not be.
func sizeField(item worktreeItem, now time.Time) string {
	switch {
	case item.size == nil:
		return label.Dash
	case item.size.err != "":
		return label.Dash + ", could not measure: " + item.size.err
	}

	return fmt.Sprintf("%s, measured %s ago", label.Bytes(item.size.bytes), label.AgeAt(item.size.measuredAt, now))
}

// lastActiveField is how long ago the directory was last touched.
func lastActiveField(item worktreeItem, now time.Time) string {
	if item.lastActiveAt.IsZero() {
		return label.Dash
	}

	field := label.AgeAt(item.lastActiveAt, now) + " ago"
	if item.agent != nil && item.agent.archivedAt != nil {
		field += ", archived"
	}

	return field
}

// buildBaseField spells where the branch stands against its base, or why git could not name one.
func buildBaseField(read worktreeInspection) recordField {
	if read.baseRef == "" {
		return recordField{label: "base", value: label.OrDash(read.baseError)}
	}
	if read.ahead == 0 && read.behind == 0 {
		return recordField{
			label:  "base",
			value:  read.baseRef + "  even",
			styled: read.baseRef + "  " + dimStyle().Render("even"),
		}
	}

	ahead := fmt.Sprintf("%s%d", glyph.Up, read.ahead)
	behind := fmt.Sprintf("%s%d", glyph.Down, read.behind)

	return recordField{
		label:  "base",
		value:  read.baseRef + "  " + ahead + " " + behind,
		styled: read.baseRef + "  " + countStyle(read.ahead, paint.Panel.Style(paint.ToneSuccess)).Render(ahead) + " " + countStyle(read.behind, paint.Panel.Style(paint.ToneWarning)).Render(behind),
	}
}

func countStyle(count int, style lipgloss.Style) lipgloss.Style {
	if count == 0 {
		return dimStyle()
	}

	return style
}

func changesField(read worktreeInspection) string {
	var parts []string
	if n := len(read.modified); n > 0 {
		parts = append(parts, label.Count(n, "modified", "modified"))
	}
	if n := len(read.untracked); n > 0 {
		parts = append(parts, label.Count(n, "untracked", "untracked"))
	}
	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, ", ")
}

// worktreeAgentField is the agent behind a directory as the record names it.
func worktreeAgentField(l listing, item worktreeItem) string {
	if item.agent == nil {
		return "none"
	}
	if live, ok := l.liveAgent(item.agent.id); ok {
		return live.Agent.Harness + ", " + string(displayState(live))
	}
	if archived, ok := l.archivedAgent(item.agent.id); ok && archived.ArchivedAt != nil {
		return archived.Harness + ", archived " + label.Day(*archived.ArchivedAt)
	}
	if item.agent.archivedAt != nil {
		return "archived " + label.Day(*item.agent.archivedAt)
	}

	return string(item.agent.state)
}

func worktreeWorkflowField(l listing, item worktreeItem) string {
	if item.agent == nil {
		return label.Dash
	}

	workflowID := ""
	if live, ok := l.liveAgent(item.agent.id); ok {
		workflowID = live.Agent.WorkflowID
	} else if archived, ok := l.archivedAgent(item.agent.id); ok {
		workflowID = archived.WorkflowID
	}

	return l.workflowName(workflowID)
}

// doc is the markdown rendered beside the list for the row the cursor is on.
func (w *worktreesScreen) doc(m *model) string {
	return w.gitDoc(m)
}

func (w *worktreesScreen) gitDoc(m *model) string {
	row, ok := rowAt(w.rows(m), w.cursor)
	switch {
	case !ok:
		return renderDocument("Worktrees", "_no worktrees_", "", "")
	case row.header:
		return renderDocument(label.Path(row.repo, homeDir()), fmt.Sprintf("_%s_", label.Count(w.countUnder(row.repo), "worktree", "worktrees")), "", "")
	}

	read, ok := m.inspections[row.item.dir]
	switch {
	case !ok || !read.answered:
		return renderDocument(row.name(), "_"+row.item.dir+"_", "_reading the directory…_", "")
	case read.err != nil && !read.isRead():
		return renderDocument(row.name(), "_"+row.item.dir+"_", "could not read: "+errui.GetReason(read.err), "")
	case read.err != nil:
		return renderDocument(row.name(), "_"+row.item.dir+"_", "_could not read: "+errui.GetReason(read.err)+"_\n\n"+worktreeProse(row.item, read), "")
	}

	return renderDocument(row.name(), "_"+row.item.dir+"_", worktreeProse(row.item, read), "")
}

// worktreeProse is the three blocks the document renders, each fenced.
func worktreeProse(item worktreeItem, read worktreeInspection) string {
	var b strings.Builder

	b.WriteString("## Contents\n\n")
	fenced(&b, entryLines(read.entries))

	if !item.readable {
		b.WriteString("\n_git cannot read this directory; its registration names a repository that is gone_\n")

		return b.String()
	}

	b.WriteString("\n## Uncommitted\n\n")
	if len(read.modified)+len(read.untracked) == 0 {
		b.WriteString("_nothing_\n")
	} else {
		fenced(&b, changeLines(read))
	}

	if read.baseRef != "" {
		fmt.Fprintf(&b, "\n## Commits since %s\n\n", read.baseRef)
		if len(read.commits) == 0 {
			b.WriteString("_none_\n")
		} else {
			fenced(&b, commitLines(read.commits))
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
func entryLines(entries []worktreeEntry) []string {
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
			rest += entry.bytes

			continue
		}

		lines = append(lines, fmt.Sprintf("%-*s  %5s", width, entryName(entry), label.Bytes(entry.bytes)))
	}
	if len(entries) > entriesShown {
		lines = append(lines, fmt.Sprintf("%-*s  %5s", width, fmt.Sprintf("+ %d more", len(entries)-entriesShown), label.Bytes(rest)))
	}

	return lines
}

func entryName(entry worktreeEntry) string {
	if entry.dir {
		return entry.path + "/"
	}

	return entry.path
}

func changeLines(read worktreeInspection) []string {
	lines := xslice.Map(read.modified, func(path string) string {
		return "M " + path
	})
	lines = append(lines, xslice.Map(read.untracked, func(path string) string {
		return "? " + path
	})...)

	return lines
}

func commitLines(commits []worktreeCommit) []string {
	lines := xslice.Map(commits, func(commit worktreeCommit) string {
		return fmt.Sprintf("%s  %-4s %s", git.ShortSHA(commit.sha), label.Age(commit.committedAt), commit.subject)
	})

	return lines
}
