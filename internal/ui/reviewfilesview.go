package ui

import (
	"fmt"
	"strings"

	"komrad/internal/diff"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	"komrad/internal/view"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// filesView is the FILES block cut to lines rows, windowed on the row the cursor is in.
func (r reviewScreen) filesView(in screen.Inputs, lines int) string {
	switch {
	case lines == 0:
		return ""
	case r.loading:
		return in.Room.Spinner + " " + paint.Panel.Style(paint.ToneMuted).Render(reviewLoadingWord) + "\n"
	case r.diff.View() == nil || r.diff.View().Len() == 0:
		return paint.Panel.Style(paint.ToneMuted).Render(reviewEmptyWord) + "\n"
	}

	width := lineWidthOf(in.Room.Width)
	tree := r.readFilesTree()
	_, onSide := r.findSide(in)
	focused := r.IsBlockFocused(in) && !onSide
	columns := measureStatColumns(r.deps.glyphs, tree.rows)
	rows := make([]string, 0, len(tree.rows))
	keys := make([]string, 0, len(tree.rows))
	for at, row := range tree.rows {
		rows = append(rows, r.renderTreeRow(renderTreeRowParams{
			Row:     row,
			Current: at == tree.current,
			Closed:  r.closedFolders[row.Path],
			Focused: focused,
			Width:   width,
			Columns: columns,
			Filter:  r.filesFilter,
			Caret:   in.Room.Caret,
		}))
		keys = append(keys, row.Path)
	}

	if in.Room.Marks {
		for at, row := range rows {
			rows[at] = markKeyedSpan(zoneFiles, keys[at], row)
		}
	}

	if len(rows) == 0 {
		rows = append(rows, paint.Panel.Style(paint.ToneMuted).Render(strings.Repeat(" ", filesGutter)+reviewNoFileWord))
	}

	header := ansi.Truncate(r.filesFilter.line(r.deps.glyphs), max(width, 0), "…")
	if in.Room.Marks {
		header = searchMark(stopBlock, width, header)
	}
	header += "\n"
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

	return header + paint.RenderList(rows, max(tree.current, 0), room, paint.Panel.Style(paint.ToneMuted))
}

// renderTreeRowParams is one row of the tree and whether it is the one the cursor is in.
type renderTreeRowParams struct {
	Row     diffview.TreeRow
	Current bool
	Closed  bool
	Focused bool
	Width   int
	Columns statColumns
	Filter  filterState
	Caret   view.Caret
}

// renderTreeRow renders one row of the tree: its mark, its place in the tree, its name, and what it
// holds.
func (r reviewScreen) renderTreeRow(params renderTreeRowParams) string {
	row, width := params.Row, params.Width
	style := lipgloss.NewStyle()
	mark := " "

	switch {
	case params.Current:
		mark = r.deps.glyphs.Spell(glyph.FileCurrent)
		style = paint.Panel.Style(paint.TonePrimary).Bold(true)
	case row.Viewed:
		style = paint.Panel.Style(paint.ToneMuted)
	}

	lead := style.Render(mark) + strings.Repeat(" ", filesGutter-1+2*row.Depth)
	if params.Current && params.Focused {
		lead = list.ArrowOf(params.Caret) + strings.Repeat(" ", filesGutter-2+2*row.Depth)
	}
	fit := fitTreeRowParams{
		Left:  params.Filter.highlight(paint.Panel, row.Name, style),
		Width: width - lipgloss.Width(lead),
	}

	if row.Folder {
		folder := glyph.FolderOpen
		if params.Closed {
			folder = glyph.FolderClosed
		}
		fit.Left = style.Render(r.deps.glyphs.Spell(folder)) + " " + fit.Left
	} else {
		fit.Stat = formatStatColumns(row.Stat, params.Columns)
	}
	if badge := formatThreadMark(r.deps.glyphs, row.Counts); badge != "" || !row.Folder {
		fit.Badges = padLeft(badge, params.Columns.Badge)
	}

	return lead + fitTreeRow(fit)
}

// statColumns is how wide each column right of a file's name is rendered across the tree.
type statColumns struct {
	Added   int
	Removed int
	Badge   int
}

// measureStatColumns is the widest count any file row of the tree renders and the widest badge any
// row does.
func measureStatColumns(glyphs glyph.Set, rows []diffview.TreeRow) statColumns {
	var columns statColumns
	for _, row := range rows {
		columns.Badge = max(columns.Badge, lipgloss.Width(formatThreadMark(glyphs, row.Counts)))
		if row.Folder {
			continue
		}
		if row.Stat.Added > 0 {
			columns.Added = max(columns.Added, lipgloss.Width(fmt.Sprintf("+%d", row.Stat.Added)))
		}
		if row.Stat.Removed > 0 {
			columns.Removed = max(columns.Removed, lipgloss.Width(fmt.Sprintf("−%d", row.Stat.Removed)))
		}
	}

	return columns
}

// formatStatColumns is a file's added and removed lines, each right aligned in its column.
func formatStatColumns(stat diff.Stat, columns statColumns) string {
	var parts []string
	if columns.Added > 0 {
		added := ""
		if stat.Added > 0 {
			added = paint.Panel.Style(reviewAddedTone).Render(fmt.Sprintf("+%d", stat.Added))
		}
		parts = append(parts, padLeft(added, columns.Added))
	}
	if columns.Removed > 0 {
		removed := ""
		if stat.Removed > 0 {
			removed = paint.Panel.Style(reviewRemovedTone).Render(fmt.Sprintf("−%d", stat.Removed))
		}
		parts = append(parts, padLeft(removed, columns.Removed))
	}

	return strings.Join(parts, " ")
}

// padLeft right aligns text in a column of width cells.
func padLeft(text string, width int) string {
	return strings.Repeat(" ", max(width-lipgloss.Width(text), 0)) + text
}

// formatThreadMark is the one glyph a file's row carries for its threads, the state that asks
// most of the reader winning.
func formatThreadMark(glyphs glyph.Set, counts diffview.ThreadCounts) string {
	switch {
	case counts.Unread > 0:
		return paint.Panel.Style(reviewHumanTone).Render(glyphs.Spell(glyph.ThreadUnread))
	case counts.Open > 0:
		return paint.Panel.Style(reviewHumanTone).Render(glyphs.Spell(glyph.ThreadRead))
	}

	return ""
}

// fitTreeRowParams is a row's name and what is rendered to the right of it.
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
