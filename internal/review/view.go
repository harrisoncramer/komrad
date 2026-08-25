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

// row is one drawable line of the review. The patch is flattened into these once, so moving
// the cursor is walking a slice rather than walking the patch.
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

// Anchor is a place in the diff, lifted off the cursor. It is what a review comment is
// attached to and what gets handed to an agent: the file, where the lines sit on each side,
// and the lines themselves so the comment can be shown with the code it is about.
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
}

func newView(patch *diff.Patch, isDark bool) *view {
	v := &view{patch: patch, sources: make([]fileSource, len(patch.Files))}

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

// isCommentable reports whether a row is one the cursor can rest on, which is a row that
// stands for a real line of a file rather than a header.
func (v *view) isCommentable(y int) bool {
	return y >= 0 && y < len(v.rows) && v.rows[y].kind == lineRow
}

// findFirst is where the cursor starts: the first row that stands for a line.
func (v *view) findFirst() int {
	return v.scan(-1, 1)
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
		if v.isCommentable(y) {
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

func (v *view) getRow(y int) (row, bool) {
	if y < 0 || y >= len(v.rows) {
		return row{}, false
	}

	return v.rows[y], true
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

// GetAnchor lifts the cursor's row into an anchor. This is the seam review comments hang off:
// everything above it draws, and everything after it is about sending what this returns.
func (v *view) GetAnchor(y int) (*Anchor, bool) {
	file, fileOK := v.getFile(y)
	line, lineOK := v.getLine(y)
	if !fileOK || !lineOK {
		return nil, false
	}

	anchor := &Anchor{
		FilePath:    file.Path(),
		OldStart:    line.OldNumber,
		OldEnd:      line.OldNumber,
		NewStart:    line.NewNumber,
		NewEnd:      line.NewNumber,
		QuotedLines: []string{markerFor(line.Kind) + line.Text},
	}

	return anchor, true
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
