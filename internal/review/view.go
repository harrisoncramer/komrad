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

// row is one drawable line of the diff. The patch is flattened into these once, so moving the
// cursor is walking a slice rather than walking the patch. A thread pinned to a line is not a
// row: it is a mark in the gutter, read in the sidebar, which is what keeps the list of lines
// fixed for as long as the diff is up.
//
// A thread with no line left to stand on is the exception. It gets a row of its own in a
// section at the foot of the diff, so the cursor can still reach it.
type row struct {
	kind rowKind

	file int
	hunk int
	line int

	text string

	thread *agentv1.ReviewThread
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

	// patchRows is how many rows the patch itself produced. Everything past it is the section of
	// threads with nowhere left to stand, rebuilt whenever the threads are pinned again. Keeping
	// that section at the end is what stops a thread arriving or drifting from moving the row
	// every other thread and the cursor are counted from.
	patchRows int

	// pinned is the thread drawn at each row, one apiece: a second thread opened on a line
	// already carrying one goes to the section at the foot instead, so that every thread is
	// somewhere the cursor can reach.
	pinned map[int]*agentv1.ReviewThread

	// placed is how each thread reached its row, by thread id.
	placed map[string]placement

	search *search
}

func NewView(patch *diff.Patch, isDark bool) *View {
	v := &View{
		patch:   patch,
		sources: make([]fileSource, len(patch.Files)),
		pinned:  map[int]*agentv1.ReviewThread{},
		placed:  map[string]placement{},
	}

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

	v.patchRows = len(v.rows)

	return v
}

// Len is how many rows the diff was flattened into.
func (v *View) Len() int { return len(v.rows) }

// Stat is the size of the patch behind the view.
func (v *View) Stat() diff.Stat { return v.patch.Stat() }

// PinThreads works out where each thread is drawn, now that the agent has been editing under
// it. A thread is never dropped for having moved: where it is read is decided here, and
// nothing else about it is.
//
// The ladder that decides a row is rowFor. A thread it cannot place, or one whose line is
// already carrying a thread, gets a row of its own at the foot of the diff rather than being
// hidden behind another conversation.
func (v *View) PinThreads(threads []*agentv1.ReviewThread) {
	v.rows = v.rows[:v.patchRows]
	v.pinned = map[int]*agentv1.ReviewThread{}
	v.placed = map[string]placement{}

	index := v.indexLines()

	var unplaced []*agentv1.ReviewThread
	for _, thread := range threads {
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

// appendAdrift gives every thread with nowhere to stand a row at the foot of the diff, under
// one line saying what they are. Sorted by where they were written so the section holds still
// between reads, since the cursor may be standing in it.
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

	v.rows = append(v.rows, row{kind: metadataRow, file: -1, hunk: -1, line: -1, text: adriftNotice})

	for _, thread := range threads {
		v.rows = append(v.rows, row{kind: threadRow, file: -1, hunk: -1, line: -1, thread: thread})
		v.pinned[len(v.rows)-1] = thread
		v.placed[thread.GetId()] = adrift
	}
}

// ThreadAt is the thread a row carries, which is what the gutter marks.
func (v *View) ThreadAt(y int) (*agentv1.ReviewThread, bool) {
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

// isPinnable reports whether a row is one the cursor can rest on: a real line of a file, or a
// thread that has no line left to be read on.
func (v *View) isPinnable(y int) bool {
	if y < 0 || y >= len(v.rows) {
		return false
	}

	return v.rows[y].kind == lineRow || v.rows[y].kind == threadRow
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

	// ThreadID is set when the cursor was standing on a thread that had come adrift rather than
	// on a line, since those rows have no coordinates to come back to.
	ThreadID string
}

// GetPlace is where the cursor is standing, in terms that survive the patch being reloaded.
func (v *View) GetPlace(y int) (Place, bool) {
	if current, ok := v.getRow(y); ok && current.kind == threadRow {
		return Place{ThreadID: current.thread.GetId()}, true
	}

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

	if place.ThreadID != "" {
		for y, current := range v.rows {
			if current.kind == threadRow && current.thread.GetId() == place.ThreadID {
				return y, true
			}
		}

		return 0, false
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

// JumpHunk moves between hunks, which is as close as a diff comes to vim's paragraph motion.
//
// Forwards is the first line of the next hunk. Backwards is the top of the hunk the cursor is
// already inside, and only the hunk before it once the cursor is standing there, the way `{`
// walks back through a paragraph rather than over it.
func (v *View) JumpHunk(y, direction int) int {
	current, ok := v.getRow(y)
	if !ok {
		return y
	}

	if direction < 0 {
		top := v.hunkTop(y, current)
		if top >= 0 && top < y {
			return top
		}

		// Standing on the top line already, so the walk starts above this hunk's own header
		// rather than finding it again and going nowhere.
		if top >= 0 {
			y = top - 1
		}
	}

	// A hunk header is not a row the cursor can rest on, so the walk looks for the header and
	// then carries on to the first line under it.
	for next := y + direction; next >= 0 && next < len(v.rows); next += direction {
		if v.rows[next].kind != hunkRow {
			continue
		}

		if landed := v.scan(next, 1); landed >= 0 {
			return landed
		}
	}

	return y
}

// hunkTop is the first line of the hunk a row is inside, or -1 for a row in no hunk at all.
func (v *View) hunkTop(y int, current row) int {
	if current.kind != lineRow {
		return -1
	}

	top := y
	for above := y - 1; above >= 0; above-- {
		if v.rows[above].kind != lineRow || v.rows[above].hunk != current.hunk || v.rows[above].file != current.file {
			break
		}

		top = above
	}

	return top
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
