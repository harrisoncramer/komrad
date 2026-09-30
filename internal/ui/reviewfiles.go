package ui

import (
	"fmt"
	"strings"

	"komrad/internal/diff"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	filesWindowFloor = 5
	filesWindowShare = 3
	filesShortColumn = 12
	filesShortWindow = 2
	filesGutter      = 2
)

// filesTree is the rows of the file tree as the sidebar draws them, and which of them the diff
// cursor is in.
type filesTree struct {
	rows    []diffview.TreeRow
	current int
}

// readFilesTree is the tree cut down to the file the cursor is in and that file's threads.
func (d reviewDiff) readFilesTree() filesTree {
	view := d.View()
	if view == nil {
		return filesTree{current: -1}
	}

	path, threadID := view.GetTreeFocus(d.Cursor())
	tree := filesTree{current: -1}

	for _, row := range view.FileTree() {
		if row.Kind == diffview.TreeThread && row.Path != path {
			continue
		}

		switch {
		case row.Kind == diffview.TreeThread && threadID != "" && row.Thread.GetId() == threadID:
			tree.current = len(tree.rows)
		case row.Kind == diffview.TreeFile && row.Path == path && threadID == "":
			tree.current = len(tree.rows)
		case row.Kind == diffview.TreeFile && row.Path == path && tree.current < 0:
			tree.current = len(tree.rows)
		}

		tree.rows = append(tree.rows, row)
	}

	return tree
}

// countFilesRows is how many rows the FILES block wants, its header included.
func (r reviewScreen) countFilesRows() int {
	if r.diff.View() == nil || r.diff.View().Len() == 0 {
		return 1
	}

	return len(r.diff.readFilesTree().rows) + 1
}

// filesFloor is the fewest rows the FILES block keeps in a column of spare rows.
func (r reviewScreen) filesFloor(spare int) int {
	floor := filesWindowFloor
	if spare < filesShortColumn {
		floor = filesShortWindow
	}

	return min(floor, r.countFilesRows())
}

// filesWindow is the rows the FILES block takes while a thread shares the column with it.
func (r reviewScreen) filesWindow(spare int) int {
	if spare < filesShortColumn {
		return r.filesFloor(spare)
	}

	return min(max(spare/filesWindowShare, filesWindowFloor), r.countFilesRows())
}

// filesView is the FILES block cut to lines rows, windowed on the row the cursor is in.
func (r reviewScreen) filesView(s panelShared, lines int) string {
	switch {
	case lines == 0:
		return ""
	case r.loading:
		return dimStyle.Render(reviewLoadingWord) + "\n"
	case r.diff.View() == nil || r.diff.View().Len() == 0:
		return dimStyle.Render(reviewEmptyWord) + "\n"
	}

	width := lineWidthOf(s.pane)
	tree := r.diff.readFilesTree()
	rows := make([]string, 0, len(tree.rows))
	for at, row := range tree.rows {
		rows = append(rows, r.diff.fileTreeRow(s, row, at == tree.current, width))
	}

	header := r.diff.filesHeader(s, width) + "\n"
	if len(rows) == 0 {
		return header
	}

	if lines < 0 {
		return header + strings.Join(rows, "\n") + "\n"
	}

	room := lines - 1
	if room <= 0 {
		return header
	}

	if room < filesShortWindow && tree.current >= 0 {
		return header + rows[tree.current] + "\n"
	}

	return header + paint.RenderList(rows, max(tree.current, 0), room, dimStyle)
}

// filesHeader names the block and says how far through the change the reader is.
func (d reviewDiff) filesHeader(s panelShared, width int) string {
	read, total := d.View().CountViewedFiles()
	left := paint.Panel.Title("FILES") + dimStyle.Render(fmt.Sprintf("  %d of %d read", read, total))

	return fitTreeRow(fitTreeRowParams{
		Left:   left,
		Stat:   dimStyle.Render(formatStat(d.View().Stat())),
		Badges: formatCounts(s.glyphs, d.View().CountThreads()),
		Width:  width,
	})
}

// fileTreeRow draws one row of the tree: its mark, its place in the tree, its name, and what it
// holds.
func (d reviewDiff) fileTreeRow(s panelShared, row diffview.TreeRow, current bool, width int) string {
	style := lipgloss.NewStyle()
	mark := " "

	switch {
	case current:
		mark = s.glyphs.Spell(glyph.FileCurrent)
		style = paint.Panel.Style(paint.ToneAccent).Bold(true)
	case row.Kind == diffview.TreeThread:
		style = paint.Panel.Style(paint.ToneAccent)
	case row.Viewed:
		mark = s.glyphs.Spell(glyph.FileViewed)
		style = dimStyle
	case row.Kind == diffview.TreeFile && d.isTreeFileCurrent(row.Path):
		style = paint.Panel.Style(paint.ToneAccent)
	}

	lead := style.Render(mark) + strings.Repeat(" ", filesGutter-1+2*row.Depth)

	var params fitTreeRowParams
	switch row.Kind {
	case diffview.TreeFolder:
		params = fitTreeRowParams{Left: style.Render(row.Name)}
	case diffview.TreeFile:
		params = fitTreeRowParams{
			Left:   style.Render(row.Name),
			Stat:   dimStyle.Render(formatStat(row.Stat)),
			Badges: formatCounts(s.glyphs, row.Counts),
		}
	case diffview.TreeThread:
		badge, badgeStyle := d.View().MarkFor(row.Thread)
		params = fitTreeRowParams{
			Left:   style.Render(formatThreadLine(row) + "  " + row.Name),
			Badges: badgeStyle.Render(badge),
		}
	}

	params.Width = width - lipgloss.Width(lead)

	return lead + fitTreeRow(params)
}

// isTreeFileCurrent reports whether the diff cursor is in a file.
func (d reviewDiff) isTreeFileCurrent(path string) bool {
	current, _ := d.View().GetTreeFocus(d.Cursor())

	return current == path
}

// formatThreadLine is the line a thread row stands on, or that it has moved off every line.
func formatThreadLine(row diffview.TreeRow) string {
	if row.Line == 0 {
		return "moved"
	}

	return fmt.Sprint(row.Line)
}

// formatStat is the lines a change added and removed, leaving out a side it did not touch.
func formatStat(stat diff.Stat) string {
	var parts []string
	if stat.Added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", stat.Added))
	}
	if stat.Removed > 0 {
		parts = append(parts, fmt.Sprintf("−%d", stat.Removed))
	}

	return strings.Join(parts, " ")
}

// formatCounts is one badge for each state that has any threads in it.
func formatCounts(glyphs glyph.Set, counts diffview.ThreadCounts) string {
	states := []struct {
		glyph glyph.Glyph
		count int
	}{
		{glyph: glyph.ThreadUnsent, count: counts.Unsent},
		{glyph: glyph.ThreadUnread, count: counts.Unread},
		{glyph: glyph.ThreadRead, count: counts.Open},
	}

	var badges []string
	for _, state := range states {
		if state.count > 0 {
			badges = append(badges, glyphs.Spell(state.glyph)+fmt.Sprint(state.count))
		}
	}

	return paint.Panel.Style(reviewHumanTone).Render(strings.Join(badges, " "))
}

// fitTreeRowParams is a row's name and what is drawn to the right of it.
type fitTreeRowParams struct {
	Left   string
	Stat   string
	Badges string
	Width  int
}

// fitTreeRow puts a row's name on the left and its counts and badges on the right, giving up the
// counts first, then the badges, then the end of the name.
func fitTreeRow(params fitTreeRowParams) string {
	rights := []string{
		joinRight(params.Stat, params.Badges),
		params.Badges,
		"",
	}

	for _, right := range rights {
		gap := params.Width - lipgloss.Width(params.Left) - lipgloss.Width(right)
		if right == "" && gap >= 0 {
			return params.Left
		}

		if right != "" && gap >= 2 {
			return params.Left + strings.Repeat(" ", gap) + right
		}
	}

	return ansi.Truncate(params.Left, max(params.Width, 0), "…")
}

// joinRight sets a row's counts and badges side by side, leaving out whichever is empty.
func joinRight(stat, badges string) string {
	switch {
	case stat == "":
		return badges
	case badges == "":
		return stat
	}

	return stat + "  " + badges
}
