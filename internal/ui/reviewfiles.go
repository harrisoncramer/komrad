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

// readFilesTree is the tree with the file the cursor is in found.
func (d reviewDiff) readFilesTree() filesTree {
	tree := filesTree{current: -1}
	view := d.View()
	if view == nil {
		return tree
	}

	path := view.GetTreeFocus(d.Cursor())
	tree.rows = view.GetFileTree()
	for at, row := range tree.rows {
		if !row.Folder && row.Path == path {
			tree.current = at

			break
		}
	}

	return tree
}

// countFilesRows is how many rows the FILES block wants, its header included.
func (r reviewScreen) countFilesRows() int {
	if r.diff.View() == nil || r.diff.View().Len() == 0 {
		return 1
	}

	return len(r.diff.View().GetFileTree()) + 1
}

// countFilesFloor is the fewest rows the FILES block keeps in a column of spare rows.
func (r reviewScreen) countFilesFloor(spare int) int {
	floor := filesWindowFloor
	if spare < filesShortColumn {
		floor = filesShortWindow
	}

	return min(floor, r.countFilesRows())
}

// filesView is the FILES block cut to lines rows, windowed on the row the cursor is in.
func (r reviewScreen) filesView(s panelShared, lines int) string {
	switch {
	case lines == 0:
		return ""
	case r.loading:
		return dimStyle().Render(reviewLoadingWord) + "\n"
	case r.diff.View() == nil || r.diff.View().Len() == 0:
		return dimStyle().Render(reviewEmptyWord) + "\n"
	}

	width := lineWidthOf(s.pane)
	tree := r.diff.readFilesTree()
	rows := make([]string, 0, len(tree.rows))
	for at, row := range tree.rows {
		rows = append(rows, renderTreeRow(s, row, at == tree.current, width))
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

	return header + paint.RenderList(rows, max(tree.current, 0), room, dimStyle())
}

// filesHeader names the block and says how far through the change the reader is.
func (d reviewDiff) filesHeader(s panelShared, width int) string {
	read, total := d.View().CountViewedFiles()
	left := paint.Panel.Title("FILES") + dimStyle().Render(fmt.Sprintf("  %d of %d read", read, total))

	return fitTreeRow(fitTreeRowParams{
		Left:   left,
		Stat:   formatStat(d.View().Stat()),
		Badges: formatCounts(s.glyphs, d.View().CountThreads()),
		Width:  width,
	})
}

// renderTreeRow draws one row of the tree: its mark, its place in the tree, its name, and what it
// holds.
func renderTreeRow(s panelShared, row diffview.TreeRow, current bool, width int) string {
	style := lipgloss.NewStyle()
	mark := " "

	switch {
	case current:
		mark = s.glyphs.Spell(glyph.FileCurrent)
		style = paint.Panel.Style(paint.TonePrimary).Bold(true)
	case row.Viewed:
		mark = s.glyphs.Spell(glyph.FileViewed)
		style = dimStyle()
	}

	lead := style.Render(mark) + strings.Repeat(" ", filesGutter-1+2*row.Depth)
	params := fitTreeRowParams{
		Left:  style.Render(row.Name),
		Width: width - lipgloss.Width(lead),
	}

	if !row.Folder {
		params.Stat = formatStat(row.Stat)
		params.Badges = formatThreadMark(s.glyphs, row.Counts)
	}

	return lead + fitTreeRow(params)
}

// formatStat is the lines a change added and removed, leaving out a side it did not touch.
func formatStat(stat diff.Stat) string {
	var parts []string
	if stat.Added > 0 {
		parts = append(parts, paint.Panel.Style(reviewAddedTone).Render(fmt.Sprintf("+%d", stat.Added)))
	}
	if stat.Removed > 0 {
		parts = append(parts, paint.Panel.Style(reviewRemovedTone).Render(fmt.Sprintf("−%d", stat.Removed)))
	}

	return strings.Join(parts, " ")
}

// formatCounts is one badge for each state that asks something of the reader and has any threads
// in it.
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

// formatThreadMark is the one glyph a file's row carries for its threads, the state that asks
// most of the reader winning.
func formatThreadMark(glyphs glyph.Set, counts diffview.ThreadCounts) string {
	switch {
	case counts.Unsent > 0:
		return paint.Panel.Style(reviewHumanTone).Render(glyphs.Spell(glyph.ThreadUnsent))
	case counts.Unread > 0:
		return paint.Panel.Style(reviewHumanTone).Render(glyphs.Spell(glyph.ThreadUnread))
	case counts.Open > 0:
		return paint.Panel.Style(reviewHumanTone).Render(glyphs.Spell(glyph.ThreadRead))
	case counts.Settled > 0:
		return dimStyle().Render(glyphs.Spell(glyph.ThreadSettled))
	}

	return ""
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
