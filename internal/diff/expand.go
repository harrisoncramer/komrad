package diff

import "strings"

// Expand draws every line of a file rather than the hunks alone, filling the runs between them
// with the unchanged lines they skip, and reports false for a file there is nothing to fill from.
func Expand(file File) (File, bool) {
	source, ok := expandableSource(file)
	if !ok {
		return file, false
	}

	filled := make([]Line, 0, len(source))
	shift, newEnd := 0, 0

	for hunkIndex := range file.Hunks {
		hunk := &file.Hunks[hunkIndex]
		filled = append(filled, gapLines(gapLinesParams{
			Source: source,
			From:   newEnd + 1,
			Until:  hunk.NewStart - 1,
			Shift:  shift,
		})...)
		filled = append(filled, hunk.Lines...)
		shift, newEnd = shiftAfter(hunk, shift), newEndOf(hunk, newEnd)
	}

	filled = append(filled, gapLines(gapLinesParams{
		Source: source,
		From:   newEnd + 1,
		Until:  len(source),
		Shift:  shift,
	})...)

	if len(filled) == countLines(file.Hunks) {
		return file, false
	}

	// One hunk covering the file, with no header.
	file.Hunks = []Hunk{{Lines: filled, NewStart: 1}}

	return file, true
}

// expandableSource is the new side of a file split into lines, and false for a file whose text
// the patch does not carry.
func expandableSource(file File) ([]string, bool) {
	if file.NewPath == "" || file.Content != "" || file.NewSource == "" || len(file.Hunks) == 0 {
		return nil, false
	}

	source := strings.Split(file.NewSource, "\n")
	if len(source) > 0 && source[len(source)-1] == "" {
		source = source[:len(source)-1]
	}

	return source, len(source) > 0
}

// gapLinesParams says which run of unchanged lines to draw and how the two sides are numbered
// across it.
type gapLinesParams struct {
	// Source is the new side of the file, a line apiece.
	Source []string
	// From and Until are the new-side lines to draw, both ends included.
	From  int
	Until int
	// Shift is how far the old side's numbering sits from the new side's over this run.
	Shift int
}

// gapLines is one run of lines both sides share, drawn from the new side and numbered on both.
func gapLines(params gapLinesParams) []Line {
	var lines []Line
	for number := max(params.From, 1); number <= min(params.Until, len(params.Source)); number++ {
		lines = append(lines, Line{
			Kind:      Context,
			Text:      params.Source[number-1],
			OldNumber: number + params.Shift,
			NewNumber: number,
		})
	}

	return lines
}

// shiftAfter is how far the old side's numbering sits from the new side's below a hunk, counted
// rather than read off either side's last line, which a hunk of nothing but additions does not
// move.
func shiftAfter(hunk *Hunk, shift int) int {
	for _, line := range hunk.Lines {
		switch line.Kind {
		case Deletion:
			shift++
		case Addition:
			shift--
		case Context:
		}
	}

	return shift
}

// newEndOf is the last line of the new side a hunk covers, which a hunk of nothing but deletions
// leaves where it was.
func newEndOf(hunk *Hunk, newEnd int) int {
	for _, line := range hunk.Lines {
		newEnd = max(newEnd, line.NewNumber)
	}

	return newEnd
}

// countLines is how many lines a file's hunks draw between them.
func countLines(hunks []Hunk) int {
	total := 0
	for _, hunk := range hunks {
		total += len(hunk.Lines)
	}

	return total
}
