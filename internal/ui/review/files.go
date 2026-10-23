package review

import (
	"cmp"
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/list"
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
func (r Screen) readFilesTree() filesTree {
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

	if r.focus != focusFiles || r.folderCursor.diffRow != r.diff.Cursor() {
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
func (r Screen) readOpenFilesTree() filesTree {
	tree := filesTree{current: -1}
	view := r.diff.View()
	if view == nil {
		return tree
	}

	path := view.GetTreeFocus(r.diff.Cursor())
	tree.rows = list.Narrowed(r.filesFilter.Pattern(), list.NarrowedParams[diffview.TreeRow]{
		Rows:  view.GetFileTree(),
		Depth: func(row diffview.TreeRow) int { return row.Depth },
		Name:  func(row diffview.TreeRow) string { return row.Name },
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
func (r *Screen) openFoldersAbove(path string) {
	for folder := range r.closedFolders {
		if strings.HasPrefix(path, folder+"/") {
			delete(r.closedFolders, folder)
		}
	}
}

// readCurrentFolder is the folder row the tree's cursor is on.
func (r Screen) readCurrentFolder() (diffview.TreeRow, bool) {
	tree := r.readFilesTree()
	if tree.current < 0 || !tree.rows[tree.current].Folder {
		return diffview.TreeRow{}, false
	}

	return tree.rows[tree.current], true
}

// restOnFolder puts the tree's cursor on a folder for as long as the diff cursor stays where it is.
func (r *Screen) restOnFolder(path string) {
	r.side = nil
	r.folderCursor = folderCursor{
		path:    path,
		diffRow: r.diff.Cursor(),
	}
}

// toggleFolder opens the folder the tree's cursor is on, or closes it when it is open.
func (r *Screen) toggleFolder() {
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
func (r Screen) countFilesRows() int {
	if r.diff.View() == nil || r.diff.View().Len() == 0 {
		return 1
	}

	return max(len(r.readFilesTree().rows), 1) + 1
}

// countFilesFloor is the fewest rows the FILES block keeps in a column of spare rows.
func (r Screen) countFilesFloor(spare int) int {
	floor := filesWindowFloor
	if spare < filesShortColumn {
		floor = filesShortWindow
	}

	return min(floor, r.countFilesRows())
}

// IsBlockRendered reports whether the file tree is rendered beside the diff.
func (r *Screen) IsBlockRendered(in screen.Inputs) bool {
	if !in.Room.OwnBlockSplit.IsDivided {
		return false
	}

	return !r.loading && r.diff.View() != nil && r.diff.View().Len() > 0
}

// IsBlockFocused reports whether the file tree holds the keys.
func (r *Screen) IsBlockFocused(in screen.Inputs) bool {
	return r.focus == focusFiles && in.Room.IsListFocused && !r.IsWriting() && r.IsBlockRendered(in)
}

// FocusBlock hands the keys to the file tree.
func (r *Screen) FocusBlock() {
	r.focus = focusFiles
	r.folderCursor = folderCursor{}
}

// BlurBlock hands the keys back to the diff.
func (r *Screen) BlurBlock() {
	r.folderCursor = folderCursor{}
	if r.focus == focusFiles {
		r.focus = focusDiff
	}
}

// stepFile moves the diff to the next file the tree renders, or the previous one for a negative
// direction.
func (r *Screen) stepFile(in screen.Inputs, direction int) screen.Request {
	r.side = nil
	diff := r.diffKeys()
	if !r.filesFilter.Pattern().IsNarrowing() && len(r.closedFolders) == 0 {
		if !diff.Pane.JumpFile(direction, diff.Window(in)) {
			return nil
		}

		return diff.Land()
	}

	return r.walkFiles(in, direction)
}

// walkFiles moves the diff down the tree by files, up it when files is negative, stopping at the
// last file there is.
func (r *Screen) walkFiles(in screen.Inputs, files int) screen.Request {
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

	return r.landOnFile(in, tree.rows[landed])
}

// filesNav is the movement keys walking the tree a row at a time while it holds the keys, the diff
// following onto each file, and the wheel walking it a file at a time while it does not.
type filesNav struct{ r *Screen }

func (f filesNav) MoveBy(in screen.Inputs, rows int) screen.Request {
	if f.r.focus != focusFiles {
		return f.r.walkFiles(in, rows)
	}

	if cmd, moved := f.r.moveSide(in, rows); moved {
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
		return f.r.stepFile(in, rows)
	}

	return f.r.landOnRow(in, row)
}

func (f filesNav) MoveToEnd(in screen.Inputs, direction int) screen.Request {
	if direction < 0 && f.r.moveSideToTop(in) {
		return nil
	}

	rows := f.r.readFilesTree().rows
	if len(rows) == 0 {
		return nil
	}

	if direction > 0 {
		return f.r.landOnRow(in, rows[len(rows)-1])
	}

	return f.r.landOnRow(in, rows[0])
}

// landOnRow puts the tree's cursor on a row, moving the diff to the top of a file and leaving it
// where it is for a folder.
func (r *Screen) landOnRow(in screen.Inputs, row diffview.TreeRow) screen.Request {
	if !row.Folder {
		return r.landOnFile(in, row)
	}

	r.restOnFolder(row.Path)

	return nil
}

// landOnFile moves the diff to the top of a file in the tree.
func (r *Screen) landOnFile(in screen.Inputs, row diffview.TreeRow) screen.Request {
	r.folderCursor = folderCursor{}
	r.side = nil
	diff := r.diffKeys()
	if !diff.Pane.JumpToFile(row.Row, diff.Window(in)) {
		return nil
	}

	return diff.Land()
}

// ClickFile lands on the file clicked in the tree, and opens or closes a folder.
func (r *Screen) ClickFile(in screen.Inputs, path string) screen.Request {
	for _, row := range r.readFilesTree().rows {
		if row.Path != path {
			continue
		}
		if !row.Folder {
			return r.landOnFile(in, row)
		}

		r.restOnFolder(row.Path)
		r.toggleFolder()

		return nil
	}

	return nil
}

func (r *Screen) openFilesFilter() {
	search := &r.filesFilter
	search.Open()
	search.OpenedIn = list.SectionList
}

// updateFilesFilter is the tree's search prompt taking keys.
func (r *Screen) updateFilesFilter(in screen.Inputs, key tea.KeyPressMsg) screen.Request {
	search := &r.filesFilter
	switch key.String() {
	case "ctrl+c":
		return screen.Hide{}
	case "tab":
		search.Mode = search.Mode.Flipped()

		return nil
	case "enter":
		search.IsTyping = false

		return r.settleFilesFilter(in)
	}

	search.TakeKey(key, r.deps.Register)

	return nil
}

// settleFilesFilter lands a search on the nearest file it matches, and a filter on the first file
// it left when it took the current one away.
func (r *Screen) settleFilesFilter(in screen.Inputs) screen.Request {
	if !r.filesFilter.Pattern().IsNarrowing() {
		return r.jumpFilesMatch(in, 0)
	}

	tree := r.readOpenFilesTree()
	if tree.current >= 0 {
		return nil
	}

	for _, row := range tree.rows {
		if !row.Folder {
			r.openFoldersAbove(row.Path)

			return r.landOnFile(in, row)
		}
	}

	return nil
}

// jumpFilesMatch walks to the next file whose name the pattern matches, the way direction says,
// wrapping at either end and leaving the cursor where it is when that file is the current one.
func (r *Screen) jumpFilesMatch(in screen.Inputs, direction int) screen.Request {
	tree := r.readOpenFilesTree()
	count := len(tree.rows)
	if r.filesFilter.Matcher == nil || count == 0 {
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
		if row.Folder || !r.filesFilter.Pattern().DoesMatch(row.Name) {
			continue
		}

		r.openFoldersAbove(row.Path)
		r.folderCursor = folderCursor{}
		if at == tree.current {
			return nil
		}

		return r.landOnFile(in, row)
	}

	return screen.NewPostMessage(screen.Warning, noFileWord)
}
