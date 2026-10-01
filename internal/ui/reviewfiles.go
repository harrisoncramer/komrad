package ui

import (
	"cmp"
	"fmt"
	"strings"

	"komrad/internal/diff"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	filesWindowFloor = 5
	filesShortColumn = 12
	filesShortWindow = 2
	filesGutter      = 2
)

// filesTree is the rows of the file tree the filter leaves, and which of them the diff cursor is
// in.
type filesTree struct {
	rows    []diffview.TreeRow
	current int
}

// readFilesTree is the tree the filter leaves with the file the cursor is in found.
func (r reviewScreen) readFilesTree() filesTree {
	tree := filesTree{current: -1}
	view := r.diff.View()
	if view == nil {
		return tree
	}

	path := view.GetTreeFocus(r.diff.Cursor())
	tree.rows = narrowed(r.filesFilter, narrowedParams[diffview.TreeRow]{
		rows:  view.GetFileTree(),
		depth: func(row diffview.TreeRow) int { return row.Depth },
		name:  func(row diffview.TreeRow) string { return row.Name },
	})
	for at, row := range tree.rows {
		if !row.Folder && row.Path == path {
			tree.current = at

			break
		}
	}

	return tree
}

// countFilesRows is how many rows the FILES block wants, its search line included.
func (r reviewScreen) countFilesRows() int {
	if r.diff.View() == nil || r.diff.View().Len() == 0 {
		return 1
	}

	return max(len(r.readFilesTree().rows), 1) + 1
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
		return s.loaderFrame + " " + dimStyle().Render(reviewLoadingWord) + "\n"
	case r.diff.View() == nil || r.diff.View().Len() == 0:
		return dimStyle().Render(reviewEmptyWord) + "\n"
	}

	width := lineWidthOf(s.pane)
	tree := r.readFilesTree()
	focused := r.isBlockFocused(s)
	columns := measureStatColumns(s.glyphs, tree.rows)
	rows := make([]string, 0, len(tree.rows))
	for at, row := range tree.rows {
		rows = append(rows, renderTreeRow(s, renderTreeRowParams{
			Row:     row,
			Current: at == tree.current,
			Focused: focused,
			Width:   width,
			Columns: columns,
			Filter:  r.filesFilter,
		}))
	}

	if len(rows) == 0 {
		rows = append(rows, dimStyle().Render(strings.Repeat(" ", filesGutter)+reviewNoFileWord))
	}

	header := ansi.Truncate(r.filesFilter.line(paint.Panel, s.glyphs), max(width, 0), "…") + "\n"
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

// renderTreeRowParams is one row of the tree and whether it is the one the cursor is in.
type renderTreeRowParams struct {
	Row     diffview.TreeRow
	Current bool
	Focused bool
	Width   int
	Columns statColumns
	Filter  filterState
}

// renderTreeRow draws one row of the tree: its mark, its place in the tree, its name, and what it
// holds.
func renderTreeRow(s panelShared, params renderTreeRowParams) string {
	row, width := params.Row, params.Width
	style := lipgloss.NewStyle()
	mark := " "

	switch {
	case params.Current:
		mark = s.glyphs.Spell(glyph.FileCurrent)
		style = paint.Panel.Style(paint.TonePrimary).Bold(true)
	case row.Viewed:
		style = dimStyle()
	}

	lead := style.Render(mark) + strings.Repeat(" ", filesGutter-1+2*row.Depth)
	if params.Current && params.Focused {
		lead = caretGlyph(s.draw.listPane(width).caret) + strings.Repeat(" ", filesGutter-2+2*row.Depth)
	}
	fit := fitTreeRowParams{
		Left:  params.Filter.highlight(paint.Panel, row.Name, style),
		Width: width - lipgloss.Width(lead),
	}

	if !row.Folder {
		fit.Stat = formatStatColumns(row.Stat, params.Columns)
		fit.Badges = padLeft(formatThreadMark(s.glyphs, row.Counts), params.Columns.Badge)
	}

	return lead + fitTreeRow(fit)
}

// statColumns is how wide each column right of a file's name is drawn across the tree.
type statColumns struct {
	Added   int
	Removed int
	Badge   int
}

// measureStatColumns is the widest count and badge any file row of the tree draws.
func measureStatColumns(glyphs glyph.Set, rows []diffview.TreeRow) statColumns {
	var columns statColumns
	for _, row := range rows {
		if row.Folder {
			continue
		}
		if row.Stat.Added > 0 {
			columns.Added = max(columns.Added, lipgloss.Width(fmt.Sprintf("+%d", row.Stat.Added)))
		}
		if row.Stat.Removed > 0 {
			columns.Removed = max(columns.Removed, lipgloss.Width(fmt.Sprintf("−%d", row.Stat.Removed)))
		}
		columns.Badge = max(columns.Badge, lipgloss.Width(formatThreadMark(glyphs, row.Counts)))
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
	case counts.Unsent > 0:
		return paint.Panel.Style(reviewHumanTone).Render(glyphs.Spell(glyph.ThreadUnsent))
	case counts.Unread > 0:
		return paint.Panel.Style(reviewHumanTone).Render(glyphs.Spell(glyph.ThreadUnread))
	case counts.Open > 0:
		return paint.Panel.Style(reviewHumanTone).Render(glyphs.Spell(glyph.ThreadRead))
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

func (r *reviewScreen) isBlockDrawn(s panelShared) bool {
	if _, ok := s.draw.screenColumnSplit(); !ok {
		return false
	}

	return !r.loading && r.diff.View() != nil && r.diff.View().Len() > 0
}

func (r *reviewScreen) isBlockFocused(s panelShared) bool {
	return r.filesFocused && s.draw.isListFocused() && !r.isWriting() && r.isBlockDrawn(s)
}

func (r *reviewScreen) focusBlock(panelShared) {
	r.filesFocused = true
}

func (r *reviewScreen) blurBlock() {
	r.filesFocused = false
}

// stepFile moves the diff to the next file the tree draws, or the previous one for a negative
// direction.
func (r *reviewScreen) stepFile(h navHost, s panelShared, direction int) tea.Cmd {
	keys := r.diffKeys()
	if !r.filesFilter.isNarrowing() {
		if !keys.pane.JumpFile(direction, keys.height(s)) {
			return nil
		}

		return keys.land(h, s)
	}

	tree := r.readFilesTree()
	at := tree.current
	if at < 0 && direction < 0 {
		at = len(tree.rows)
	}

	for at += direction; at >= 0 && at < len(tree.rows); at += direction {
		if !tree.rows[at].Folder {
			return r.landOnFile(h, s, tree.rows[at])
		}
	}

	return nil
}

// landOnFile moves the diff to the top of a file in the tree.
func (r *reviewScreen) landOnFile(h navHost, s panelShared, row diffview.TreeRow) tea.Cmd {
	keys := r.diffKeys()
	if !keys.pane.JumpToFile(row.Row, keys.height(s)) {
		return nil
	}

	return keys.land(h, s)
}

func (r *reviewScreen) openFilesFilter() {
	filter := &r.filesFilter
	filter.typing = true
	filter.mode = filterModeSearch
	filter.setQuery("")
}

// updateFilesFilter is the tree's search prompt taking keys.
func (r *reviewScreen) updateFilesFilter(h host, s panelShared, key tea.KeyPressMsg) tea.Cmd {
	filter := &r.filesFilter
	switch key.String() {
	case "ctrl+c":
		return h.hide()
	case "tab":
		filter.mode = filter.mode.flipped()

		return nil
	case "enter":
		filter.typing = false

		return r.settleFilesFilter(h, s)
	}

	filter.takeKey(key, s.register)

	return nil
}

// settleFilesFilter lands a search on the nearest file it matches, and a filter on the first file
// it left when it took the current one away.
func (r *reviewScreen) settleFilesFilter(h navHost, s panelShared) tea.Cmd {
	if !r.filesFilter.isNarrowing() {
		return r.jumpFilesMatch(h, s, 0)
	}

	tree := r.readFilesTree()
	if tree.current >= 0 {
		return nil
	}

	for _, row := range tree.rows {
		if !row.Folder {
			return r.landOnFile(h, s, row)
		}
	}

	return nil
}

// jumpFilesMatch walks to the next file whose name the pattern matches, the way direction says,
// wrapping at either end and leaving the cursor where it is when that file is the current one.
func (r *reviewScreen) jumpFilesMatch(h navHost, s panelShared, direction int) tea.Cmd {
	tree := r.readFilesTree()
	n := len(tree.rows)
	if r.filesFilter.pattern == nil || n == 0 {
		return nil
	}

	walk := cmp.Or(direction, 1)
	origin, first := tree.current, 1
	switch {
	case origin >= 0 && direction == 0:
		first = 0
	case origin < 0 && walk < 0:
		origin = n
	}

	for offset := first; offset <= n; offset++ {
		at := ((origin+walk*offset)%n + n) % n
		row := tree.rows[at]
		if row.Folder || !r.filesFilter.doesMatch(row.Name) {
			continue
		}

		if at == tree.current {
			return nil
		}

		return r.landOnFile(h, s, row)
	}

	h.note(messageWarning, reviewNoFileWord)

	return nil
}

// filesFilterDesc names the search key by the pattern standing over the tree.
func (r *reviewScreen) filesFilterDesc() string {
	if r.filesFilter.isStanding() {
		return r.filesFilter.mode.String() + ": " + r.filesFilter.query()
	}

	return "search"
}
