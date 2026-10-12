package ui

import (
	"cmp"
	"slices"

	"komrad/internal/ui/diffview"

	tea "charm.land/bubbletea/v2"
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

func (r *reviewScreen) isBlockRendered(m *model) bool {
	if _, ok := m.screenColumnSplit(); !ok {
		return false
	}

	return !r.loading && r.diff.View() != nil && r.diff.View().Len() > 0
}

func (r *reviewScreen) isBlockFocused(m *model) bool {
	return r.focus == reviewFocusFiles && m.isListFocused() && !r.isWriting() && r.isBlockRendered(m)
}

func (r *reviewScreen) focusBlock(*model) {
	r.focus = reviewFocusFiles
}

func (r *reviewScreen) blurBlock() {
	if r.focus == reviewFocusFiles {
		r.focus = reviewFocusDiff
	}
}

// stepFile moves the diff to the next file the tree renders, or the previous one for a negative
// direction.
func (r *reviewScreen) stepFile(m *model, direction int) tea.Cmd {
	keys := r.diffKeys()
	if !r.filesFilter.isNarrowing() {
		if !keys.pane.JumpFile(direction, keys.window(m)) {
			return nil
		}

		return keys.land(m)
	}

	return r.walkFiles(m, direction)
}

// walkFiles moves the diff down the tree by files, up it when files is negative, stopping at the
// last file there is.
func (r *reviewScreen) walkFiles(m *model, files int) tea.Cmd {
	direction := 1
	if files < 0 {
		direction = -1
	}

	tree := r.readFilesTree()
	at := tree.current
	if at < 0 && direction < 0 {
		at = len(tree.rows)
	}

	landed := -1
	for at += direction; at >= 0 && at < len(tree.rows) && files != 0; at += direction {
		if !tree.rows[at].Folder {
			landed = at
			files -= direction
		}
	}

	if landed < 0 {
		return nil
	}

	return r.landOnFile(m, tree.rows[landed])
}

// filesNav is the movement keys walking the diff a file at a time from the file tree.
type filesNav struct{ r *reviewScreen }

func (f filesNav) moveBy(m *model, files int) tea.Cmd {
	if files == 1 || files == -1 {
		return f.r.stepFile(m, files)
	}

	return f.r.walkFiles(m, files)
}

func (f filesNav) moveToEnd(m *model, direction int) tea.Cmd {
	rows := slices.Clone(f.r.readFilesTree().rows)
	if direction > 0 {
		slices.Reverse(rows)
	}

	for _, row := range rows {
		if !row.Folder {
			return f.r.landOnFile(m, row)
		}
	}

	return nil
}

// landOnFile moves the diff to the top of a file in the tree.
func (r *reviewScreen) landOnFile(m *model, row diffview.TreeRow) tea.Cmd {
	keys := r.diffKeys()
	if !keys.pane.JumpToFile(row.Row, keys.window(m)) {
		return nil
	}

	return keys.land(m)
}

func (r *reviewScreen) clickFile(m *model, path string) tea.Cmd {
	for _, row := range r.readFilesTree().rows {
		if !row.Folder && row.Path == path {
			return r.landOnFile(m, row)
		}
	}

	return nil
}

func (r *reviewScreen) openFilesFilter() {
	filter := &r.filesFilter
	filter.open()
	filter.openedIn = sectionList
}

// updateFilesFilter is the tree's search prompt taking keys.
func (r *reviewScreen) updateFilesFilter(m *model, key tea.KeyPressMsg) tea.Cmd {
	filter := &r.filesFilter
	switch key.String() {
	case "ctrl+c":
		return m.hide()
	case "tab":
		filter.mode = filter.mode.flipped()

		return nil
	case "enter":
		filter.typing = false

		return r.settleFilesFilter(m)
	}

	filter.takeKey(key, m.register)

	return nil
}

// settleFilesFilter lands a search on the nearest file it matches, and a filter on the first file
// it left when it took the current one away.
func (r *reviewScreen) settleFilesFilter(m *model) tea.Cmd {
	if !r.filesFilter.isNarrowing() {
		return r.jumpFilesMatch(m, 0)
	}

	if r.readFilesTree().current >= 0 {
		return nil
	}

	return r.stepFile(m, 1)
}

// jumpFilesMatch walks to the next file whose name the pattern matches, the way direction says,
// wrapping at either end and leaving the cursor where it is when that file is the current one.
func (r *reviewScreen) jumpFilesMatch(m *model, direction int) tea.Cmd {
	tree := r.readFilesTree()
	count := len(tree.rows)
	if r.filesFilter.pattern == nil || count == 0 {
		return nil
	}

	walk := cmp.Or(direction, 1)
	origin, first := tree.current, 1
	switch {
	case origin >= 0 && direction == 0:
		first = 0
	case origin < 0 && walk < 0:
		origin = count
	}

	for offset := first; offset <= count; offset++ {
		at := ((origin+walk*offset)%count + count) % count
		row := tree.rows[at]
		if row.Folder || !r.filesFilter.doesMatch(row.Name) {
			continue
		}

		if at == tree.current {
			return nil
		}

		return r.landOnFile(m, row)
	}

	m.note(messageWarning, reviewNoFileWord)

	return nil
}
