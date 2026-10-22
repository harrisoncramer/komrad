package ui

import (
	"cmp"
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

const (
	filesWindowFloor = 5
	filesShortColumn = 12
	filesShortWindow = 2
	filesGutter      = 2
)

// filesTree is the rows of the file tree the filter leaves, and which of them the tree's cursor is
// on.
type filesTree struct {
	rows    []diffview.TreeRow
	current int
}

// folderCursor is the folder row the tree's cursor rests on, held while the tree holds the keys and
// the diff cursor stays on the row it was on when the tree's cursor got there.
type folderCursor struct {
	path    string
	diffRow int
}

// readFilesTree is the tree the filter leaves with each closed folder standing in for the rows
// under it.
func (r reviewScreen) readFilesTree() filesTree {
	open := r.readOpenFilesTree()
	path := ""
	if open.current >= 0 {
		path = open.rows[open.current].Path
	}

	tree := filesTree{current: -1}
	hiding := -1
	for _, row := range open.rows {
		if hiding >= 0 && row.Depth > tree.rows[hiding].Depth {
			tree.rows[hiding].Counts = tree.rows[hiding].Counts.Plus(row.Counts)

			continue
		}

		hiding = -1
		if row.Folder && r.closedFolders[row.Path] {
			hiding = len(tree.rows)
		}
		if row.Path == path || (hiding >= 0 && strings.HasPrefix(path, row.Path+"/")) {
			tree.current = len(tree.rows)
		}

		tree.rows = append(tree.rows, row)
	}

	if r.focus != reviewFocusFiles || r.folderCursor.diffRow != r.diff.Cursor() {
		return tree
	}

	for at, row := range tree.rows {
		if row.Folder && row.Path == r.folderCursor.path {
			tree.current = at
		}
	}

	return tree
}

// readOpenFilesTree is the tree the filter leaves with every folder open and the file the cursor
// is in found.
func (r reviewScreen) readOpenFilesTree() filesTree {
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

// openFoldersAbove opens every folder of the tree a file sits in.
func (r *reviewScreen) openFoldersAbove(path string) {
	for folder := range r.closedFolders {
		if strings.HasPrefix(path, folder+"/") {
			delete(r.closedFolders, folder)
		}
	}
}

// readCurrentFolder is the folder row the tree's cursor is on.
func (r reviewScreen) readCurrentFolder() (diffview.TreeRow, bool) {
	tree := r.readFilesTree()
	if tree.current < 0 || !tree.rows[tree.current].Folder {
		return diffview.TreeRow{}, false
	}

	return tree.rows[tree.current], true
}

// restOnFolder puts the tree's cursor on a folder for as long as the diff cursor stays where it is.
func (r *reviewScreen) restOnFolder(path string) {
	r.side = nil
	r.folderCursor = folderCursor{
		path:    path,
		diffRow: r.diff.Cursor(),
	}
}

// toggleFolder opens the folder the tree's cursor is on, or closes it when it is open.
func (r *reviewScreen) toggleFolder() {
	folder, ok := r.readCurrentFolder()
	if !ok {
		return
	}

	r.restOnFolder(folder.Path)

	if r.closedFolders[folder.Path] {
		delete(r.closedFolders, folder.Path)

		return
	}

	if r.closedFolders == nil {
		r.closedFolders = map[string]bool{}
	}
	r.closedFolders[folder.Path] = true
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

func (r *reviewScreen) isBlockRendered(m *model, in screen.Inputs) bool {
	if !in.Room.Split.On {
		return false
	}

	return !r.loading && r.diff.View() != nil && r.diff.View().Len() > 0
}

func (r *reviewScreen) isBlockFocused(m *model, in screen.Inputs) bool {
	return r.focus == reviewFocusFiles && in.Room.ListFocused && !r.isWriting() && r.isBlockRendered(m, in)
}

func (r *reviewScreen) focusBlock(*model) {
	r.focus = reviewFocusFiles
	r.folderCursor = folderCursor{}
}

func (r *reviewScreen) blurBlock() {
	r.folderCursor = folderCursor{}
	if r.focus == reviewFocusFiles {
		r.focus = reviewFocusDiff
	}
}

// stepFile moves the diff to the next file the tree renders, or the previous one for a negative
// direction.
func (r *reviewScreen) stepFile(m *model, direction int) screen.Request {
	r.side = nil
	keys := r.diffKeys()
	if !r.filesFilter.isNarrowing() && len(r.closedFolders) == 0 {
		if !keys.pane.JumpFile(direction, keys.window(m)) {
			return nil
		}

		return keys.land(m)
	}

	return r.walkFiles(m, direction)
}

// walkFiles moves the diff down the tree by files, up it when files is negative, stopping at the
// last file there is.
func (r *reviewScreen) walkFiles(m *model, files int) screen.Request {
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

// filesNav is the movement keys walking the tree a row at a time while it holds the keys, the diff
// following onto each file, and the wheel walking it a file at a time while it does not.
type filesNav struct{ r *reviewScreen }

func (f filesNav) moveBy(m *model, rows int) screen.Request {
	if f.r.focus != reviewFocusFiles {
		return f.r.walkFiles(m, rows)
	}

	if cmd, moved := f.r.moveSide(m, rows); moved {
		return cmd
	}

	tree := f.r.readFilesTree()
	if len(tree.rows) == 0 {
		return nil
	}

	at := tree.current
	if at < 0 && rows < 0 {
		at = len(tree.rows)
	}

	next := min(max(at+rows, 0), len(tree.rows)-1)
	if next == tree.current {
		return nil
	}

	row := tree.rows[next]
	isFileToFile := tree.current >= 0 && !tree.rows[tree.current].Folder && !row.Folder
	if isFileToFile && (rows == 1 || rows == -1) {
		return f.r.stepFile(m, rows)
	}

	return f.r.landOnRow(m, row)
}

func (f filesNav) moveToEnd(m *model, direction int) screen.Request {
	if direction < 0 && f.r.moveSideToTop(m) {
		return nil
	}

	rows := f.r.readFilesTree().rows
	if len(rows) == 0 {
		return nil
	}

	if direction > 0 {
		return f.r.landOnRow(m, rows[len(rows)-1])
	}

	return f.r.landOnRow(m, rows[0])
}

// landOnRow puts the tree's cursor on a row, moving the diff to the top of a file and leaving it
// where it is for a folder.
func (r *reviewScreen) landOnRow(m *model, row diffview.TreeRow) screen.Request {
	if !row.Folder {
		return r.landOnFile(m, row)
	}

	r.restOnFolder(row.Path)

	return nil
}

// landOnFile moves the diff to the top of a file in the tree.
func (r *reviewScreen) landOnFile(m *model, row diffview.TreeRow) screen.Request {
	r.folderCursor = folderCursor{}
	r.side = nil
	keys := r.diffKeys()
	if !keys.pane.JumpToFile(row.Row, keys.window(m)) {
		return nil
	}

	return keys.land(m)
}

func (r *reviewScreen) clickFile(m *model, path string) screen.Request {
	for _, row := range r.readFilesTree().rows {
		if row.Path != path {
			continue
		}
		if !row.Folder {
			return r.landOnFile(m, row)
		}

		r.restOnFolder(row.Path)
		r.toggleFolder()

		return nil
	}

	return nil
}

func (r *reviewScreen) openFilesFilter() {
	filter := &r.filesFilter
	filter.open()
	filter.openedIn = sectionList
}

// updateFilesFilter is the tree's search prompt taking keys.
func (r *reviewScreen) updateFilesFilter(m *model, key tea.KeyPressMsg) screen.Request {
	filter := &r.filesFilter
	switch key.String() {
	case "ctrl+c":
		return screen.Hide{}
	case "tab":
		filter.mode = filter.mode.flipped()

		return nil
	case "enter":
		filter.typing = false

		return r.settleFilesFilter(m)
	}

	filter.takeKey(key, r.deps.register)

	return nil
}

// settleFilesFilter lands a search on the nearest file it matches, and a filter on the first file
// it left when it took the current one away.
func (r *reviewScreen) settleFilesFilter(m *model) screen.Request {
	if !r.filesFilter.isNarrowing() {
		return r.jumpFilesMatch(m, 0)
	}

	tree := r.readOpenFilesTree()
	if tree.current >= 0 {
		return nil
	}

	for _, row := range tree.rows {
		if !row.Folder {
			r.openFoldersAbove(row.Path)

			return r.landOnFile(m, row)
		}
	}

	return nil
}

// jumpFilesMatch walks to the next file whose name the pattern matches, the way direction says,
// wrapping at either end and leaving the cursor where it is when that file is the current one.
func (r *reviewScreen) jumpFilesMatch(m *model, direction int) screen.Request {
	tree := r.readOpenFilesTree()
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

		r.openFoldersAbove(row.Path)
		r.folderCursor = folderCursor{}
		if at == tree.current {
			return nil
		}

		return r.landOnFile(m, row)
	}

	return screen.NewNote(screen.Warning, reviewNoFileWord)
}
