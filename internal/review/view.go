// Package review flattens an agent's diff into a list of drawable rows, pins review threads
// to the lines they were written against, and draws one row at a time.
package review

import (
	"weave/internal/diff"
	agentv1 "weave/proto/gen/agent/v1"
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
// drawn as a mark in the gutter and read in the sidebar, which is what keeps this list fixed
// for as long as the diff is up.
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

type View struct {
	patch *diff.Patch

	rows    []row
	sources []fileSource

	// pinned is the thread anchored at each place, keyed by file and line. Only threads whose
	// quoted line still matches the diff are in here, so a mark is never drawn against code
	// that has changed since the thread was written.
	pinned map[anchorKey]*agentv1.ReviewThread
}

func NewView(patch *diff.Patch, isDark bool) *View {
	v := &View{patch: patch, sources: make([]fileSource, len(patch.Files)), pinned: map[anchorKey]*agentv1.ReviewThread{}}

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

// Len is how many rows the diff was flattened into.
func (v *View) Len() int { return len(v.rows) }

// PinThreads works out which threads still point at the code they were written against.
//
// A thread carries the line it was about, so the check is that quote against the line now at
// those coordinates. Matching pins the thread and the gutter marks it. Not matching leaves it
// unpinned: the agent has rewritten that code, and marking the line anyway would point at
// something the thread never mentioned. An unpinned thread is not lost: the cursor simply
// never lands on a mark for it.
func (v *View) PinThreads(threads []*agentv1.ReviewThread) {
	v.pinned = map[anchorKey]*agentv1.ReviewThread{}

	quoted := map[anchorKey]string{}
	for _, current := range v.rows {
		if current.kind != lineRow {
			continue
		}

		line := v.patch.Files[current.file].Hunks[current.hunk].Lines[current.line]
		quoted[anchorKey{
			filePath: v.patch.Files[current.file].Path(),
			newLine:  int32(line.NewNumber),
			oldLine:  int32(line.OldNumber),
		}] = markerFor(line.Kind) + line.Text
	}

	for _, thread := range threads {
		key := keyForThread(thread)

		here, ok := quoted[key]
		if !ok || len(thread.GetQuotedLines()) == 0 || thread.GetQuotedLines()[0] != here {
			continue
		}

		v.pinned[key] = thread
	}
}

// IsPinned reports whether a thread still points at the code it was written against, which is
// what decides whether the cursor can ever reach it.
func (v *View) IsPinned(thread *agentv1.ReviewThread) bool {
	pinned, ok := v.pinned[keyForThread(thread)]

	return ok && pinned.GetId() == thread.GetId()
}

// ThreadAt is the thread pinned to the line a row stands for, if the anchor still holds.
func (v *View) ThreadAt(y int) (*agentv1.ReviewThread, bool) {
	key, ok := v.getKey(y)
	if !ok {
		return nil, false
	}

	thread, ok := v.pinned[key]

	return thread, ok
}

// isPinnable reports whether a row is one the cursor can rest on, which is a row standing for
// a real line of a file rather than a header.
func (v *View) isPinnable(y int) bool {
	return y >= 0 && y < len(v.rows) && v.rows[y].kind == lineRow
}

// First is where the cursor starts: the first row that stands for a line.
func (v *View) First() int {
	return max(v.scan(-1, 1), 0)
}

// Place is a spot in the diff worth returning to, taken off the cursor before the patch is
// read again so the reader is not thrown back to the top of it.
type Place struct {
	FilePath string

	NewLine int
	OldLine int
}

// GetPlace is where the cursor is standing, in terms that survive the patch being reloaded.
func (v *View) GetPlace(y int) (Place, bool) {
	key, ok := v.getKey(y)
	if !ok {
		return Place{}, false
	}

	return Place{FilePath: key.filePath, NewLine: int(key.newLine), OldLine: int(key.oldLine)}, true
}

// Locate is the row standing where place was, in a patch that has since been read again.
//
// The exact line is preferred, but a commit or an edit moves lines around, so a file that is
// still in the diff falls back to whichever of its rows is nearest the line the reader left.
// That is a guess, and it is a better one than the top of the diff.
func (v *View) Locate(place Place) (int, bool) {
	exact, nearest, distance := -1, -1, 0

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

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}

// Last is the last row the cursor can rest on.
func (v *View) Last() int {
	return max(v.scan(len(v.rows), -1), 0)
}

// Move walks to the next row the cursor can rest on, and stays put when there is none.
func (v *View) Move(y, direction int) int {
	if next := v.scan(y, direction); next >= 0 {
		return next
	}

	return y
}

// Page jumps by a number of drawn rows and settles on the nearest row the cursor can rest on,
// looking forward first so landing on a file or hunk header never eats the keypress. The jump
// is measured in rows drawn rather than rows the cursor can hold, so a page moves what was
// actually on screen.
func (v *View) Page(y, delta int) int {
	target := min(max(y+delta, 0), max(len(v.rows)-1, 0))
	if v.isPinnable(target) {
		return target
	}

	forward, backward := 1, -1
	if delta < 0 {
		forward, backward = -1, 1
	}

	if next := v.scan(target, forward); next >= 0 {
		return next
	}
	if next := v.scan(target, backward); next >= 0 {
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

	for index, candidate := range v.rows {
		if candidate.file == target && candidate.kind == lineRow {
			return index
		}
	}

	return y
}

// JumpThread moves to the next or previous line carrying a thread.
func (v *View) JumpThread(y, direction int) int {
	for next := y + direction; next >= 0 && next < len(v.rows); next += direction {
		if _, ok := v.ThreadAt(next); ok {
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
	if !ok {
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

// EditTarget is the file the cursor is standing in and the line to open it on. A deleted line
// has no place in the file as it stands, so it answers with the nearest line that does, and a
// deleted file answers with nothing to open at all.
func (v *View) EditTarget(y int) (string, int, bool) {
	file, ok := v.getFile(y)
	if !ok || file.NewPath == "" {
		return "", 0, false
	}

	return file.NewPath, v.nearestNewLine(y), true
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
