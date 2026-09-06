// Package review flattens an agent's diff into a list of drawable rows, pins review threads
// to the lines they were written against, and draws one row at a time.
package review

import (
	"cmp"
	"slices"

	"weave/internal/diff"
	agentv1 "weave/proto/gen/agent/v1"
)

// adriftNotice heads the section of threads with no line left to stand on.
const adriftNotice = "── threads with no line left to stand on ──"

type rowKind uint8

const (
	fileRow rowKind = iota
	metadataRow
	hunkRow
	lineRow
	threadRow
)

// row is one drawable line of the diff, flattened from the patch once so that moving the
// cursor walks a slice rather than the patch.
type row struct {
	kind   rowKind
	file   int
	hunk   int
	line   int
	text   string
	thread *agentv1.ReviewThread
}

// fileSource is both sides of one file, lexed, since a deletion is drawn from the old side
// and everything else from the new one.
type fileSource struct {
	old []styledLine
	new []styledLine
}

// Anchor is the place a thread is pinned to, holding the file, both sides' line numbers, and
// the lines themselves, so it still says what it was about once the code is rewritten.
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
	rows    []row
	sources []fileSource

	// patchRows is how many rows the patch produced, and keeping the adrift threads past that
	// point stops one arriving or drifting from moving every other row.
	patchRows int

	// pinned is the thread drawn at each row, one apiece, and a second thread on the same line
	// goes to the foot instead so the cursor can still reach it.
	pinned map[int]*agentv1.ReviewThread

	// placed is how each thread reached its row, by thread id.
	placed map[string]placement
	// viewed is the files the person reviewing has read, by path, which are drawn collapsed.
	viewed map[string]bool
	// threads is what was last pinned, kept so that collapsing a file can pin them again over
	// the rows that rebuilding left.
	threads []*agentv1.ReviewThread
	search  *search
}

// NewView builds the review view over a patch, highlighting each file for the theme the
// terminal is on.
func NewView(patch *diff.Patch, isDark bool) *View {
	v := &View{
		patch:   patch,
		sources: make([]fileSource, len(patch.Files)),
		pinned:  map[int]*agentv1.ReviewThread{},
		placed:  map[string]placement{},
		viewed:  map[string]bool{},
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
// else, since the point of marking one is to get it out of the way until something changes in
// it.
func (v *View) build() {
	v.rows = nil

	for fileIndex := range v.patch.Files {
		file := &v.patch.Files[fileIndex]

		v.rows = append(v.rows, row{
			kind: fileRow,
			file: fileIndex,
			hunk: -1,
			line: -1,
			text: file.Path(),
		})

		if v.viewed[file.Path()] {
			continue
		}

		for _, metadata := range file.Metadata {
			v.rows = append(v.rows, row{
				kind: metadataRow,
				file: fileIndex,
				hunk: -1,
				line: -1,
				text: metadata,
			})
		}

		for hunkIndex := range file.Hunks {
			hunk := &file.Hunks[hunkIndex]
			v.rows = append(v.rows, row{
				kind: hunkRow,
				file: fileIndex,
				hunk: hunkIndex,
				line: -1,
				text: hunk.Header,
			})
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

// SetViewed says which files have been read, collapsing each of those to its header and pinning
// the threads again over the rows that survive it.
func (v *View) SetViewed(paths map[string]bool) {
	v.viewed = paths
	if v.viewed == nil {
		v.viewed = map[string]bool{}
	}

	v.build()
	v.PinThreads(v.threads)
}

// SetFileViewed folds one file away once it has been read, or opens it again.
func (v *View) SetFileViewed(path string, viewed bool) {
	if viewed {
		v.viewed[path] = true
	} else {
		delete(v.viewed, path)
	}

	v.build()
	v.PinThreads(v.threads)
}

// IsViewed reports whether a file is drawn collapsed, which is what the toggle reads to know
// which way it is going.
func (v *View) IsViewed(path string) bool { return v.viewed[path] }

// CountViewedFiles returns how many of the patch's files have been read, and how many there are.
func (v *View) CountViewedFiles() (int, int) {
	read := 0
	for _, file := range v.patch.Files {
		if v.viewed[file.Path()] {
			read++
		}
	}

	return read, len(v.patch.Files)
}

// GetFileAt returns the file the cursor is inside, which is what a mark of having read one is put on.
func (v *View) GetFileAt(y int) (diff.File, bool) { return v.getFile(y) }

// isCollapsed reports whether a file is drawn as its header alone.
func (v *View) isCollapsed(fileIndex int) bool {
	if fileIndex < 0 || fileIndex >= len(v.patch.Files) {
		return false
	}

	return v.viewed[v.patch.Files[fileIndex].Path()]
}

// Len is how many rows the diff was flattened into.
func (v *View) Len() int { return len(v.rows) }

// Stat is the size of the patch behind the view.
func (v *View) Stat() diff.Stat { return v.patch.Stat() }

// Base is what the diff was measured against.
func (v *View) Base() string { return v.patch.BaseSHA }

// PinThreads works out where each thread is drawn now that the agent has been editing under
// it, and never drops one for having moved.
func (v *View) PinThreads(threads []*agentv1.ReviewThread) {
	v.threads = threads
	v.rows = v.rows[:v.patchRows]
	v.pinned = map[int]*agentv1.ReviewThread{}
	v.placed = map[string]placement{}

	index := v.indexLines()

	var unplaced []*agentv1.ReviewThread
	for _, thread := range threads {
		// A thread in a collapsed file has a line to stand on the moment the file is opened
		// again, so it is not adrift and does not go to the foot of the diff.
		if v.viewed[thread.GetFilePath()] {
			v.placed[thread.GetId()] = collapsed

			continue
		}

		y, where := v.rowFor(index, thread)
		if where == adrift || v.pinned[y] != nil {
			unplaced = append(unplaced, thread)

			continue
		}

		v.pinned[y] = thread
		v.placed[thread.GetId()] = where
	}

	v.appendAdrift(unplaced)

	// The foot section is rebuilt here, so any search holding row numbers from the old one is
	// run again rather than left pointing at rows that have moved.
	if v.search != nil {
		v.SetPattern(v.search.pattern)
	}
}

// appendAdrift gives every thread with nowhere to stand a row at the foot of the diff, sorted
// by where it was written so the section holds still while the cursor is in it.
func (v *View) appendAdrift(threads []*agentv1.ReviewThread) {
	if len(threads) == 0 {
		return
	}

	slices.SortFunc(threads, func(a, b *agentv1.ReviewThread) int {
		return cmp.Or(
			cmp.Compare(a.GetFilePath(), b.GetFilePath()),
			cmp.Compare(lineOf(a), lineOf(b)),
			cmp.Compare(a.GetId(), b.GetId()),
		)
	})

	v.rows = append(v.rows, row{
		kind: metadataRow,
		file: -1,
		hunk: -1,
		line: -1,
		text: adriftNotice,
	})

	for _, thread := range threads {
		v.rows = append(v.rows, row{
			kind:   threadRow,
			file:   -1,
			hunk:   -1,
			line:   -1,
			thread: thread,
		})
		v.pinned[len(v.rows)-1] = thread
		v.placed[thread.GetId()] = adrift
	}
}

// GetThreadAt returns the thread a row carries, which is what the gutter marks.
func (v *View) GetThreadAt(y int) (*agentv1.ReviewThread, bool) {
	thread, ok := v.pinned[y]

	return thread, ok
}

// HasMoved reports whether a thread is drawn anywhere other than the line it was written
// against, which is what decides whether the sidebar repeats the line it quoted.
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
// thread that has no line left to be read on, or the header of a collapsed file, which is the
// only row that file has left to be opened again from, though a search jump lands on an expanded
// file header too.
func (v *View) isPinnable(y int) bool {
	if y < 0 || y >= len(v.rows) {
		return false
	}

	current := v.rows[y]

	return current.kind == lineRow ||
		current.kind == threadRow ||
		(current.kind == fileRow && v.isCollapsed(current.file))
}

// GetFirstRow returns where the cursor starts: the first row that stands for a line.
func (v *View) GetFirstRow() int {
	return max(v.scan(-1, 1), 0)
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

	// ThreadID is set when the cursor was standing on a thread that had come adrift rather than
	// on a line, since those rows have no coordinates to come back to.
	ThreadID string
}

// GetPlace is where the cursor is standing, in terms that survive the patch being reloaded.
func (v *View) GetPlace(y int) (Place, bool) {
	current, ok := v.getRow(y)
	if ok && current.kind == threadRow {
		return Place{ThreadID: current.thread.GetId()}, true
	}

	// A file header has no line under the cursor, so the file itself is the place.
	if ok && current.kind == fileRow {
		return Place{
			FilePath: v.patch.Files[current.file].Path(),
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

// Locate is the row standing where place was in a patch that has since been read again,
// falling back to the nearest row of the same file when the exact line has moved.
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

	if place.Header {
		return v.fileHeader(place.FilePath)
	}

	// A file read since the cursor was last in it is drawn as its header alone, which is the
	// only row left of it to come back to.
	if y, ok := v.collapsedHeader(place.FilePath); ok {
		return y, true
	}

	for y, current := range v.rows {
		if current.kind != lineRow || v.patch.Files[current.file].Path() != place.FilePath {
			continue
		}

		line := v.patch.Files[current.file].Hunks[current.hunk].Lines[current.line]
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

// collapsedHeader is the header row of a file drawn collapsed, and false for a file drawn
// whole, which has lines of its own to stand on.
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
		if current.kind == fileRow && v.patch.Files[current.file].Path() == path {
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
	if target < 0 || target >= len(v.patch.Files) {
		return y
	}

	// The first row of the file the cursor can rest on, which is its first line, or its header
	// alone when the file has been read and folded away.
	for index, candidate := range v.rows {
		if candidate.file == target && v.isPinnable(index) {
			return index
		}
	}

	return y
}

// GetFileTopRow returns the row the file at y starts on, so a jump into it can put its header at the top
// of the pane rather than leaving the scroll margin to decide.
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

func (v *View) getRow(y int) (row, bool) {
	if y < 0 || y >= len(v.rows) {
		return row{}, false
	}

	return v.rows[y], true
}

// getKey is where the line under a row sits, which is what a thread is pinned to.
func (v *View) getKey(y int) (anchorKey, bool) {
	current, ok := v.getRow(y)
	if !ok || current.kind != lineRow {
		return anchorKey{}, false
	}

	line := v.patch.Files[current.file].Hunks[current.hunk].Lines[current.line]

	return anchorKey{
		filePath: v.patch.Files[current.file].Path(),
		newLine:  int32(line.NewNumber),
		oldLine:  int32(line.OldNumber),
	}, true
}

// getFile is the file the cursor is inside, header rows included.
func (v *View) getFile(y int) (diff.File, bool) {
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

	return v.patch.Files[current.file].Hunks[current.hunk].Lines[current.line], true
}

// GetAnchor lifts the cursor's row into an anchor, which is what a new thread is pinned to.
func (v *View) GetAnchor(y int) (*Anchor, bool) {
	file, fileOK := v.getFile(y)
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

// getStyledLine is the lexed source behind a diff line, which comes off whichever side of the
// file the line belongs to.
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
	file, ok := v.getFile(y)
	if !ok || file.NewPath == "" {
		return "", 0, false
	}

	return file.NewPath, v.nearestNewLine(y), true
}

// GetSource is the round's own copy of a file, and nothing for one the patch draws no text of,
// since a binary and an omitted file have no lines to compare an edit against.
func (v *View) GetSource(filePath string) (string, bool) {
	file, ok := v.findFile(filePath)
	if !ok || file.Content != "" || file.NewPath == "" {
		return "", false
	}

	return file.NewSource, true
}

// GetRangeAnchor is the anchor covering a run of new-side lines, and nothing for a run the diff
// does not draw whole, since a comment cannot be pinned to lines that are not on screen.
func (v *View) GetRangeAnchor(filePath string, newStart, newEnd int) (*Anchor, bool) {
	file, ok := v.findFile(filePath)
	if !ok {
		return nil, false
	}

	anchor := &Anchor{FilePath: file.Path()}
	for hunkIndex := range file.Hunks {
		for _, line := range file.Hunks[hunkIndex].Lines {
			if line.NewNumber < newStart || line.NewNumber > newEnd {
				continue
			}

			if anchor.NewStart == 0 {
				anchor.NewStart = line.NewNumber
			}

			anchor.NewEnd = line.NewNumber
			if line.OldNumber > 0 {
				if anchor.OldStart == 0 {
					anchor.OldStart = line.OldNumber
				}

				anchor.OldEnd = line.OldNumber
			}

			anchor.QuotedLines = append(anchor.QuotedLines, markerFor(line.Kind)+line.Text)
		}
	}

	if len(anchor.QuotedLines) != newEnd-newStart+1 {
		return nil, false
	}

	return anchor, true
}

func (v *View) findFile(filePath string) (*diff.File, bool) {
	for fileIndex := range v.patch.Files {
		if v.patch.Files[fileIndex].Path() == filePath {
			return &v.patch.Files[fileIndex], true
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
