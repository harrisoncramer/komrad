// Package diffview flattens an agent's diff into a list of drawable rows, pins review threads
// to the lines they were written against, and draws one row at a time.
package diffview

import (
	"maps"
	"slices"

	"komrad/internal/diff"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

type rowKind uint8

const (
	fileRow rowKind = iota
	metadataRow
	hunkRow
	lineRow
	threadRow
	changeRow
)

// changeWord is what the row above the first file says, the place a comment on the change as a
// whole is written from.
const changeWord = "the change as a whole"

// row is one drawable line of the diff, flattened from the patch once so that moving the cursor
// walks a slice rather than the patch.
type row struct {
	kind   rowKind
	file   int
	hunk   int
	line   int
	text   string
	thread *agentv1.ReviewThread
}

// fileSource is both sides of one file, lexed, with a deletion drawn from the old side and
// everything else from the new one.
type fileSource struct {
	old []styledLine
	new []styledLine
}

// Anchor is the place a thread is pinned to, holding the file, both sides' line numbers and the
// lines themselves, so it still says what it was about once the code is rewritten.
type Anchor struct {
	FilePath    string
	OldStart    int
	OldEnd      int
	NewStart    int
	NewEnd      int
	QuotedLines []string
}

type View struct {
	patch   *diff.Patch
	traced  map[string]Traced
	rows    []row
	sources []fileSource

	// patchRows is how many rows the patch produced, the adrift threads being kept past that point
	// so one arriving does not move every other row.
	patchRows int

	// pinned is the thread drawn at each row, one apiece, a second thread on the same line going
	// to the foot instead.
	pinned map[int]*agentv1.ReviewThread

	// placed is how each thread reached its row, by thread id.
	placed map[string]placement
	// files is the patch's files as they are drawn, an expanded one holding every line of itself
	// rather than its hunks alone.
	files []diff.File
	// viewed is the files the person reviewing has read, by path, which are drawn collapsed.
	viewed map[string]bool
	// expanded is the files the person reviewing has opened out, by path.
	expanded map[string]bool
	// whole is each file with every line of it drawn, built the first time it is asked for and
	// kept.
	whole map[string]*diff.File
	// threads is what was last pinned, kept so collapsing a file can pin them again over the rows
	// that rebuilding left.
	threads []*agentv1.ReviewThread
	search  *search
	// narrowed says the patch is a slice of the change rather than the whole of it.
	narrowed bool
	// headless leaves out the row for the change as a whole and each file's header.
	headless bool
}

// SetHeadless draws the hunks alone, with no row for the change as a whole and no file headers.
func (v *View) SetHeadless(headless bool) {
	v.headless = headless
	v.redraw()
}

// SetNarrowed says whether the patch on screen is a slice of the change rather than all of it.
func (v *View) SetNarrowed(narrowed bool) {
	v.narrowed = narrowed
}

// NewView builds the review view over a patch, highlighting each file for the theme the
// terminal is on.
func NewView(patch *diff.Patch, isDark bool) *View {
	v := &View{
		patch:    patch,
		sources:  make([]fileSource, len(patch.Files)),
		pinned:   map[int]*agentv1.ReviewThread{},
		placed:   map[string]placement{},
		files:    slices.Clone(patch.Files),
		viewed:   map[string]bool{},
		expanded: map[string]bool{},
		whole:    map[string]*diff.File{},
	}

	highlight := newHighlighter(isDark)
	for fileIndex := range patch.Files {
		file := &patch.Files[fileIndex]
		v.sources[fileIndex] = fileSource{
			old: highlight.splitLines(file.Path(), file.OldSource),
			new: highlight.splitLines(file.Path(), file.NewSource),
		}
	}

	v.build()

	return v
}

// build flattens the patch into rows, giving a file that has been read its header and nothing
// else.
func (v *View) build() {
	v.rows = nil

	if len(v.files) > 0 && !v.headless {
		v.rows = append(v.rows, row{
			kind: changeRow,
			file: -1,
			hunk: -1,
			line: -1,
			text: changeWord,
		})
	}

	for fileIndex := range v.files {
		file := &v.files[fileIndex]

		if !v.headless {
			v.rows = append(v.rows, row{
				kind: fileRow,
				file: fileIndex,
				hunk: -1,
				line: -1,
				text: file.Path(),
			})
		}

		if v.viewed[file.Path()] {
			continue
		}

		v.rows = append(v.rows, xslice.Map(file.Metadata, func(metadata string) row {
			return row{
				kind: metadataRow,
				file: fileIndex,
				hunk: -1,
				line: -1,
				text: metadata,
			}
		})...)

		for hunkIndex := range file.Hunks {
			hunk := &file.Hunks[hunkIndex]
			if hunk.Header != "" {
				v.rows = append(v.rows, row{
					kind: hunkRow,
					file: fileIndex,
					hunk: hunkIndex,
					line: -1,
					text: hunk.Header,
				})
			}
			for lineIndex := range hunk.Lines {
				v.rows = append(v.rows, row{
					kind: lineRow,
					file: fileIndex,
					hunk: hunkIndex,
					line: lineIndex,
				})
			}
		}
	}

	v.patchRows = len(v.rows)
}

// GetBaseSHA is the commit the old side of this patch was measured from.
func (v *View) GetBaseSHA() string {
	return v.patch.BaseSHA
}

// GetSnapshotSHA is the commit the new side of this patch was measured to.
func (v *View) GetSnapshotSHA() string {
	return v.patch.SnapshotSHA
}

// SetViewed says which files have been read, collapsing each of those to its header and pinning
// the threads again over the rows that survive it.
func (v *View) SetViewed(paths map[string]bool) {
	v.viewed = paths
	if v.viewed == nil {
		v.viewed = map[string]bool{}
	}

	v.redraw()
}

// SetFileViewed folds one file away once it has been read, or opens it again.
func (v *View) SetFileViewed(path string, viewed bool) {
	if viewed {
		v.viewed[path] = true
	} else {
		delete(v.viewed, path)
	}

	v.SetViewed(v.viewed)
}

// IsViewed reports whether a file is drawn collapsed.
func (v *View) IsViewed(path string) bool {
	return v.viewed[path]
}

// SetExpanded says which files are drawn whole, for a view built again over a patch the reader
// had already opened files in.
func (v *View) SetExpanded(paths map[string]bool) {
	v.expanded = paths
	if v.expanded == nil {
		v.expanded = map[string]bool{}
	}

	v.redraw()
}

// SetFileExpanded draws one file whole, or gives back its hunks alone.
func (v *View) SetFileExpanded(path string, expanded bool) {
	if !expanded {
		delete(v.expanded, path)
		v.redraw()

		return
	}

	if _, ok := v.wholeFile(path); !ok {
		return
	}

	v.expanded[path] = true
	v.redraw()
}

// IsExpanded reports whether a file is drawn whole.
func (v *View) IsExpanded(path string) bool {
	return v.expanded[path]
}

// GetExpanded is the files drawn whole, by path, as a copy a view built later can be given.
func (v *View) GetExpanded() map[string]bool {
	return maps.Clone(v.expanded)
}

// wholeFile is a file with every line of it drawn, and false for one there is nothing to add to.
func (v *View) wholeFile(path string) (*diff.File, bool) {
	if found, ok := v.whole[path]; ok {
		return found, found != nil
	}

	v.whole[path] = nil

	file, ok := v.patchFile(path)
	if !ok {
		return nil, false
	}

	expanded, ok := diff.Expand(*file)
	if !ok {
		return nil, false
	}

	v.whole[path] = &expanded

	return &expanded, true
}

// redraw builds the files as they are drawn, flattens them into rows, and puts the threads back
// on the rows they now belong to.
func (v *View) redraw() {
	for index := range v.patch.Files {
		path := v.patch.Files[index].Path()

		v.files[index] = v.patch.Files[index]
		if !v.expanded[path] {
			continue
		}

		if whole, ok := v.wholeFile(path); ok {
			v.files[index] = *whole
		}
	}

	v.build()
	v.PinThreads(v.threads)
}

// patchFile is a file as the patch holds it, before anything the reader has opened out.
func (v *View) patchFile(path string) (*diff.File, bool) {
	for index := range v.patch.Files {
		if v.patch.Files[index].Path() == path {
			return &v.patch.Files[index], true
		}
	}

	return nil, false
}

// CountViewedFiles returns how many of the patch's files have been read, and how many there are.
func (v *View) CountViewedFiles() (int, int) {
	read := 0
	for _, file := range v.files {
		if v.viewed[file.Path()] {
			read++
		}
	}

	return read, len(v.files)
}

// isCollapsed reports whether a file is drawn as its header alone.
func (v *View) isCollapsed(fileIndex int) bool {
	if fileIndex < 0 || fileIndex >= len(v.files) {
		return false
	}

	return v.viewed[v.files[fileIndex].Path()]
}

// Len is how many rows the diff was flattened into.
func (v *View) Len() int {
	return len(v.rows)
}

// Stat is the size of the patch behind the view.
func (v *View) Stat() diff.Stat {
	return v.patch.Stat()
}

// GetFile reports whether the patch on screen draws a file at all.
func (v *View) GetFile(filePath string) (*diff.File, bool) {
	return v.findFile(filePath)
}

// CommentPosition is where one comment stands among the comments drawn, counting down the diff.
func (v *View) CommentPosition(thread *agentv1.ReviewThread) (int, int) {
	rows := make([]int, 0, len(v.pinned))
	for y := range v.pinned {
		rows = append(rows, y)
	}

	slices.Sort(rows)

	for at, y := range rows {
		if v.pinned[y].GetId() == thread.GetId() {
			return at + 1, len(rows)
		}
	}

	return 0, len(rows)
}

// GetThreadAt returns the thread a row carries.
func (v *View) GetThreadAt(y int) (*agentv1.ReviewThread, bool) {
	thread, ok := v.pinned[y]

	return thread, ok
}

// HasMoved reports whether a thread is drawn anywhere other than the line it was written
// against.
func (v *View) HasMoved(thread *agentv1.ReviewThread) bool {
	where, ok := v.placed[thread.GetId()]

	return !ok || where != pinned
}

// IsGhosted reports whether a thread is drawn on a line whose content has changed under it,
// which is the one thing about placement the gutter has to say out loud: the line beside the
// mark is not the line the conversation is about.
func (v *View) IsGhosted(thread *agentv1.ReviewThread) bool {
	return v.placed[thread.GetId()] == ghosted
}

// isPinnable reports whether stepping the cursor may leave it on a row: a real line of a file, a
// thread that has no line left to be read on, or the header of a collapsed file.
func (v *View) isPinnable(y int) bool {
	if y < 0 || y >= len(v.rows) {
		return false
	}

	current := v.rows[y]

	return current.kind == lineRow ||
		current.kind == threadRow ||
		current.kind == changeRow ||
		(current.kind == fileRow && v.isCollapsed(current.file))
}

// GetFirstRow returns where the cursor starts: the first row that stands for a line, below the
// row for the change as a whole.
func (v *View) GetFirstRow() int {
	first := max(v.scan(-1, 1), 0)
	if current, ok := v.getRow(first); ok && current.kind == changeRow {
		if next := v.scan(first, 1); next >= 0 {
			return next
		}
	}

	return first
}

// Place is a spot in the diff worth returning to, taken off the cursor before the patch is
// read again so the reader is not thrown back to the top of it.
type Place struct {
	FilePath string
	NewLine  int
	OldLine  int

	// Header is set when the cursor was standing on the header of a file drawn whole, which a
	// search by file name lands on and which has no line under it to come back to, and left
	// unset for a collapsed file so that opening that file again puts the cursor into it.
	Header bool

	// ThreadID is set when the cursor was standing on a thread that had come adrift rather than on
	// a line, those rows having no coordinates to come back to.
	ThreadID string

	// Change is set when the cursor was standing on the row for the change as a whole.
	Change bool
}

// GetPlace is where the cursor is standing, in terms that survive the patch being reloaded.
func (v *View) GetPlace(y int) (Place, bool) {
	current, ok := v.getRow(y)
	if ok && current.kind == threadRow {
		return Place{ThreadID: current.thread.GetId()}, true
	}

	if ok && current.kind == changeRow {
		return Place{Change: true}, true
	}

	// A file header has no line under the cursor, so the file itself is the place.
	if ok && current.kind == fileRow {
		return Place{
			FilePath: v.files[current.file].Path(),
			Header:   !v.isCollapsed(current.file),
		}, true
	}

	key, ok := v.getKey(y)
	if !ok {
		return Place{}, false
	}

	return Place{
		FilePath: key.filePath,
		NewLine:  int(key.newLine),
		OldLine:  int(key.oldLine),
	}, true
}

// Locate is the row standing where place was in a patch that has since been read again, falling
// back to the nearest row of the same file when the exact line has moved.
func (v *View) Locate(place Place) (int, bool) {
	exact, nearest, distance := -1, -1, 0

	if place.ThreadID != "" {
		for y, current := range v.rows {
			if current.kind == threadRow && current.thread.GetId() == place.ThreadID {
				return y, true
			}
		}

		return 0, false
	}

	if place.Change {
		return v.findChangeRow()
	}

	if place.Header {
		return v.fileHeader(place.FilePath)
	}

	// A file read since the cursor was last in it is drawn as its header alone.
	if y, ok := v.collapsedHeader(place.FilePath); ok {
		return y, true
	}

	for y, current := range v.rows {
		if current.kind != lineRow || v.files[current.file].Path() != place.FilePath {
			continue
		}

		line := v.files[current.file].Hunks[current.hunk].Lines[current.line]
		if line.NewNumber == place.NewLine && line.OldNumber == place.OldLine {
			exact = y

			break
		}

		gap := abs(line.NewNumber - place.NewLine)
		if nearest < 0 || gap < distance {
			nearest, distance = y, gap
		}
	}

	if exact >= 0 {
		return exact, true
	}

	return nearest, nearest >= 0
}

// findChangeRow finds the row for the change as a whole, which a patch with no files has none of.
func (v *View) findChangeRow() (int, bool) {
	for y, current := range v.rows {
		if current.kind == changeRow {
			return y, true
		}
	}

	return 0, false
}

// collapsedHeader is the header row of a file drawn collapsed, and false for a file drawn
// whole.
func (v *View) collapsedHeader(path string) (int, bool) {
	if !v.viewed[path] {
		return 0, false
	}

	return v.fileHeader(path)
}

// fileHeader is the row a file is named on, which every file has whether it is drawn collapsed
// or whole.
func (v *View) fileHeader(path string) (int, bool) {
	if path == "" {
		return 0, false
	}

	for y, current := range v.rows {
		if current.kind == fileRow && v.files[current.file].Path() == path {
			return y, true
		}
	}

	return 0, false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

// GetLastRow returns the last row the cursor can rest on.
func (v *View) GetLastRow() int {
	return max(v.scan(len(v.rows), -1), 0)
}

// Move walks to the next row the cursor can rest on, and stays put when there is none.
func (v *View) Move(y, direction int) int {
	if next := v.scan(y, direction); next >= 0 {
		return next
	}

	return y
}

func (v *View) scan(from, direction int) int {
	for y := from + direction; y >= 0 && y < len(v.rows); y += direction {
		if v.isPinnable(y) {
			return y
		}
	}

	return -1
}

// JumpFile moves to the first line of the next or previous file.
func (v *View) JumpFile(y, direction int) int {
	current, ok := v.getRow(y)
	if !ok {
		return y
	}

	target := current.file + direction
	if target < 0 || target >= len(v.files) {
		return y
	}

	// The first row of the file the cursor can rest on: its first line, or its header alone
	// when the file has been read and folded away.
	for index, candidate := range v.rows {
		if candidate.file == target && v.isPinnable(index) {
			return index
		}
	}

	return y
}

// GetFileTopRow returns the row the file at y starts on, so a jump into it can put its header at
// the top of the pane.
func (v *View) GetFileTopRow(y int) int {
	current, ok := v.getRow(y)
	if !ok {
		return y
	}

	for index, candidate := range v.rows {
		if candidate.file == current.file {
			return index
		}
	}

	return y
}

// JumpThread moves to the next or previous line carrying a thread.
func (v *View) JumpThread(y, direction int) int {
	for next := y + direction; next >= 0 && next < len(v.rows); next += direction {
		if _, ok := v.GetThreadAt(next); ok {
			return next
		}
	}

	return y
}

// JumpOpenThread moves to the next or previous line carrying a thread nobody has settled, which
// is what a person clearing a review walks.
func (v *View) JumpOpenThread(y, direction int) (int, bool) {
	for next := y + direction; next >= 0 && next < len(v.rows); next += direction {
		if thread, ok := v.GetThreadAt(next); ok && isUnsettled(thread) {
			return next, true
		}
	}

	return y, false
}

// IsOpenThreadAt reports whether a row carries a comment of the person's that nobody has settled.
func (v *View) IsOpenThreadAt(y int) bool {
	thread, ok := v.GetThreadAt(y)

	return ok && isUnsettled(thread)
}

// CountOpenThreads is how many conversations the person reviewing still owes an answer on, drawn
// or folded away.
func (v *View) CountOpenThreads() int {
	open := 0

	for _, thread := range v.threads {
		if isUnsettled(thread) {
			open++
		}
	}

	return open
}

func (v *View) getRow(y int) (row, bool) {
	if y < 0 || y >= len(v.rows) {
		return row{}, false
	}

	return v.rows[y], true
}

// getKey is where the line under a row sits.
func (v *View) getKey(y int) (anchorKey, bool) {
	current, ok := v.getRow(y)
	if !ok || current.kind != lineRow {
		return anchorKey{}, false
	}

	line := v.files[current.file].Hunks[current.hunk].Lines[current.line]

	return anchorKey{
		filePath: v.files[current.file].Path(),
		newLine:  int32(line.NewNumber),
		oldLine:  int32(line.OldNumber),
	}, true
}

// GetDrawnFileAt is the file the cursor is inside as it is drawn, header rows included.
func (v *View) GetDrawnFileAt(y int) (diff.File, bool) {
	current, ok := v.getRow(y)
	if !ok || current.file < 0 {
		return diff.File{}, false
	}

	return v.files[current.file], true
}

// GetPatchFileAt is the file the cursor is inside as the patch holds it, before anything the
// reader has opened out.
func (v *View) GetPatchFileAt(y int) (diff.File, bool) {
	current, ok := v.getRow(y)
	if !ok || current.file < 0 {
		return diff.File{}, false
	}

	return v.patch.Files[current.file], true
}

// getLine is the diff line under the cursor.
func (v *View) getLine(y int) (diff.Line, bool) {
	current, ok := v.getRow(y)
	if !ok || current.kind != lineRow {
		return diff.Line{}, false
	}

	return v.files[current.file].Hunks[current.hunk].Lines[current.line], true
}

// GetAnchor lifts the cursor's row into an anchor, one naming no file for the row that stands for
// the change as a whole.
func (v *View) GetAnchor(y int) (*Anchor, bool) {
	if current, ok := v.getRow(y); ok && current.kind == changeRow {
		return &Anchor{}, true
	}

	file, fileOK := v.GetDrawnFileAt(y)
	line, lineOK := v.getLine(y)
	if !fileOK || !lineOK {
		return nil, false
	}

	return &Anchor{
		FilePath:    file.Path(),
		OldStart:    line.OldNumber,
		OldEnd:      line.OldNumber,
		NewStart:    line.NewNumber,
		NewEnd:      line.NewNumber,
		QuotedLines: []string{markerFor(line.Kind) + line.Text},
	}, true
}

func markerFor(kind diff.LineKind) string {
	switch kind {
	case diff.Addition:
		return "+"
	case diff.Deletion:
		return "-"
	case diff.Context:
	}

	return " "
}

// getStyledLine is the lexed source behind a diff line, taken from whichever side of the file
// the line belongs to.
func (v *View) getStyledLine(current row, line diff.Line) styledLine {
	source := v.sources[current.file]
	if line.Kind == diff.Deletion {
		return lineAt(source.old, line.OldNumber)
	}

	return lineAt(source.new, line.NewNumber)
}

// GetEditTarget returns the file the cursor is in and the line to open it on, answering with the
// nearest surviving line when the cursor is on a deleted one.
func (v *View) GetEditTarget(y int) (string, int, bool) {
	file, ok := v.GetDrawnFileAt(y)
	if !ok || file.NewPath == "" {
		return "", 0, false
	}

	return file.NewPath, v.nearestNewLine(y), true
}

func (v *View) findFile(filePath string) (*diff.File, bool) {
	for fileIndex := range v.files {
		if v.files[fileIndex].Path() == filePath {
			return &v.files[fileIndex], true
		}
	}

	return nil, false
}

// nearestNewLine walks out from a row to the closest line that still exists on the new side,
// looking back before forward so a run of deletions lands on the code above it.
func (v *View) nearestNewLine(y int) int {
	if line, ok := v.getLine(y); ok && line.NewNumber > 0 {
		return line.NewNumber
	}

	current, ok := v.getRow(y)
	if !ok {
		return 1
	}

	for _, direction := range []int{-1, 1} {
		for next := y + direction; next >= 0 && next < len(v.rows) && v.rows[next].file == current.file; next += direction {
			if line, ok := v.getLine(next); ok && line.NewNumber > 0 {
				return line.NewNumber
			}
		}
	}

	return 1
}

// GetThreadRow is the row a thread is drawn on, false for one no row carries.
func (v *View) GetThreadRow(id string) (int, bool) {
	for y, thread := range v.pinned {
		if thread.GetId() == id {
			return y, true
		}
	}

	return 0, false
}

// IsFoldedAway reports whether a thread stands in a file the reader has read and folded away.
func (v *View) IsFoldedAway(thread *agentv1.ReviewThread) bool {
	return v.placed[thread.GetId()] == collapsed
}
