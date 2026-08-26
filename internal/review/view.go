package review

import (
	"weave/internal/diff"
)

type rowKind uint8

const (
	fileRow rowKind = iota
	metadataRow
	hunkRow
	lineRow
)

// row is one drawable line of the diff. The patch is flattened into these once, so moving the
// cursor is walking a slice rather than walking the patch. Threads are not rows: they are
// drawn as a mark in the gutter and read on a screen of their own, which is what keeps this
// list fixed for the life of the viewer.
type row struct {
	kind rowKind

	file int
	hunk int
	line int

	text string
}

// fileSource is both sides of one file, lexed. A deletion is drawn from the old side and
// everything else from the new one, since that is where each line actually lives.
type fileSource struct {
	old []styledLine
	new []styledLine
}

// Anchor is a place in the diff, lifted off the cursor. It is what a review thread is pinned
// to: the file, where the lines sit on each side, and the lines themselves, so the thread
// still says what it was about once the agent has rewritten that code.
type Anchor struct {
	FilePath string

	OldStart int
	OldEnd   int

	NewStart int
	NewEnd   int

	QuotedLines []string
}

type view struct {
	patch *diff.Patch

	rows    []row
	sources []fileSource

	// pinned is the threads anchored at each place, keyed by file and line. Only threads whose
	// quoted line still matches the diff are in here, so a mark is never drawn against code
	// that has changed since the thread was written. A line can hold more than one, since
	// nothing stops a second thread being opened on a line that already has one.
	pinned map[anchorKey][]Thread
}

func newView(patch *diff.Patch, isDark bool) *view {
	v := &view{patch: patch, sources: make([]fileSource, len(patch.Files)), pinned: map[anchorKey][]Thread{}}

	highlight := newHighlighter(isDark)
	for fileIndex := range patch.Files {
		file := &patch.Files[fileIndex]
		v.sources[fileIndex] = fileSource{
			old: highlight.splitLines(file.Path(), file.OldSource),
			new: highlight.splitLines(file.Path(), file.NewSource),
		}

		v.rows = append(v.rows, row{kind: fileRow, file: fileIndex, hunk: -1, line: -1, text: file.Path()})
		for _, metadata := range file.Metadata {
			v.rows = append(v.rows, row{kind: metadataRow, file: fileIndex, hunk: -1, line: -1, text: metadata})
		}

		for hunkIndex := range file.Hunks {
			hunk := &file.Hunks[hunkIndex]
			v.rows = append(v.rows, row{kind: hunkRow, file: fileIndex, hunk: hunkIndex, line: -1, text: hunk.Header})
			for lineIndex := range hunk.Lines {
				v.rows = append(v.rows, row{kind: lineRow, file: fileIndex, hunk: hunkIndex, line: lineIndex})
			}
		}
	}

	return v
}

// pinThreads works out which threads still point at the code they were written against.
//
// A thread carries the line it was about, so the check is that quote against the line now at
// those coordinates. Matching pins the thread and the gutter marks it. Not matching leaves it
// unpinned: the agent has rewritten that code, and marking the line anyway would point at
// something the thread never mentioned. An unpinned thread is not lost, it is simply read on
// the thread screen under the place it was written rather than against the diff.
func (v *view) pinThreads(threads []Thread) {
	v.pinned = map[anchorKey][]Thread{}

	quoted := map[anchorKey]string{}
	for _, current := range v.rows {
		if current.kind != lineRow {
			continue
		}

		line := v.patch.Files[current.file].Hunks[current.hunk].Lines[current.line]
		quoted[anchorKey{
			filePath: v.patch.Files[current.file].Path(),
			newLine:  line.NewNumber,
			oldLine:  line.OldNumber,
		}] = markerFor(line.Kind) + line.Text
	}

	for _, thread := range threads {
		key := keyForThread(thread)

		here, ok := quoted[key]
		if !ok || len(thread.QuotedLines) == 0 || thread.QuotedLines[0] != here {
			continue
		}

		v.pinned[key] = append(v.pinned[key], thread)
	}
}

// getThreads is every thread pinned to the line a row stands for, in the order they were
// listed. More than one is ordinary: pressing c twice on a line opens two conversations about
// it, and both have to be reachable.
func (v *view) getThreads(y int) []Thread {
	key, ok := v.getKey(y)
	if !ok {
		return nil
	}

	return v.pinned[key]
}

// isPinnable reports whether a row is one the cursor can rest on, which is a row standing for
// a real line of a file rather than a header.
func (v *view) isPinnable(y int) bool {
	return y >= 0 && y < len(v.rows) && v.rows[y].kind == lineRow
}

// findFirst is where the cursor starts: the first row that stands for a line.
func (v *view) findFirst() int {
	return max(v.scan(-1, 1), 0)
}

// findLast is the last row the cursor can rest on.
func (v *view) findLast() int {
	return max(v.scan(len(v.rows), -1), 0)
}

// move walks to the next row the cursor can rest on, and stays put when there is none.
func (v *view) move(y, direction int) int {
	if next := v.scan(y, direction); next >= 0 {
		return next
	}

	return y
}

func (v *view) scan(from, direction int) int {
	for y := from + direction; y >= 0 && y < len(v.rows); y += direction {
		if v.isPinnable(y) {
			return y
		}
	}

	return -1
}

// jumpFile moves to the first line of the next or previous file.
func (v *view) jumpFile(y, direction int) int {
	current, ok := v.getRow(y)
	if !ok {
		return y
	}

	target := current.file + direction
	if target < 0 || target >= len(v.patch.Files) {
		return y
	}

	for index, candidate := range v.rows {
		if candidate.file == target && candidate.kind == lineRow {
			return index
		}
	}

	return y
}

// jumpThread moves to the next or previous line carrying a thread.
func (v *view) jumpThread(y, direction int) int {
	for next := y + direction; next >= 0 && next < len(v.rows); next += direction {
		if len(v.getThreads(next)) > 0 {
			return next
		}
	}

	return y
}

// findAnchor is where a thread sits in the diff, so the cursor can be taken to it from the
// thread screen. It looks for that thread among the pinned ones rather than for its
// coordinates, since a stale thread's line number now belongs to whatever the agent wrote
// there instead, and taking the cursor to unrelated code is worse than not moving. It answers
// -1 for a thread whose anchor no longer holds.
func (v *view) findAnchor(thread Thread) int {
	key := keyForThread(thread)
	for index := range v.rows {
		found, ok := v.getKey(index)
		if !ok || found != key {
			continue
		}

		for _, pinned := range v.pinned[key] {
			if pinned.ID == thread.ID {
				return index
			}
		}
	}

	return -1
}

func (v *view) getRow(y int) (row, bool) {
	if y < 0 || y >= len(v.rows) {
		return row{}, false
	}

	return v.rows[y], true
}

// getKey is where the line under a row sits, which is what a thread is pinned to.
func (v *view) getKey(y int) (anchorKey, bool) {
	current, ok := v.getRow(y)
	if !ok || current.kind != lineRow {
		return anchorKey{}, false
	}

	line := v.patch.Files[current.file].Hunks[current.hunk].Lines[current.line]

	return anchorKey{
		filePath: v.patch.Files[current.file].Path(),
		newLine:  line.NewNumber,
		oldLine:  line.OldNumber,
	}, true
}

// getFile is the file the cursor is inside, header rows included.
func (v *view) getFile(y int) (diff.File, bool) {
	current, ok := v.getRow(y)
	if !ok {
		return diff.File{}, false
	}

	return v.patch.Files[current.file], true
}

// getLine is the diff line under the cursor.
func (v *view) getLine(y int) (diff.Line, bool) {
	current, ok := v.getRow(y)
	if !ok || current.kind != lineRow {
		return diff.Line{}, false
	}

	return v.patch.Files[current.file].Hunks[current.hunk].Lines[current.line], true
}

// GetAnchor lifts the cursor's row into an anchor, which is what a new thread is pinned to.
func (v *view) GetAnchor(y int) (*Anchor, bool) {
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
func (v *view) getStyledLine(current row, line diff.Line) styledLine {
	source := v.sources[current.file]
	if line.Kind == diff.Deletion {
		return lineAt(source.old, line.OldNumber)
	}

	return lineAt(source.new, line.NewNumber)
}
