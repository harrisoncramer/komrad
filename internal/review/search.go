package review

import (
	"regexp"
	"slices"
)

// span is where a match sits inside a row's text, as byte offsets into the drawn line including
// the marker the diff put in front of it.
type span struct {
	start int
	end   int
}

// search is a pattern and the rows it matches.
type search struct {
	pattern *regexp.Regexp
	matches map[int][]span
	// rows is every matching row in order.
	rows []int
}

// SetPattern finds the rows a pattern matches and returns how many there are, compiling it
// through the panel's own filter.
func (v *View) SetPattern(pattern *regexp.Regexp) int {
	if pattern == nil {
		v.ClearSearch()

		return 0
	}

	found := &search{pattern: pattern, matches: map[int][]span{}}
	for y := range v.rows {
		text, ok := v.matchText(y)
		if !ok {
			continue
		}

		for _, at := range pattern.FindAllStringIndex(text, -1) {
			found.matches[y] = append(found.matches[y], span{start: at[0], end: at[1]})
		}

		if len(found.matches[y]) > 0 {
			found.rows = append(found.rows, y)
		}
	}

	v.search = found

	return len(found.rows)
}

// ClearSearch forgets the pattern and everything it found.
func (v *View) ClearSearch() { v.search = nil }

// MatchCount is how many lines the pattern was found on, counting several matches on one line
// once.
func (v *View) MatchCount() int {
	if v.search == nil {
		return 0
	}

	return len(v.search.rows)
}

// HiddenMatches is how many lines the pattern was found on inside files that have been read and
// folded away, which are on no row for the search to step through.
func (v *View) HiddenMatches() int {
	if v.search == nil {
		return 0
	}

	hidden := 0
	for _, file := range v.files {
		if !v.viewed[file.Path()] {
			continue
		}

		for _, hunk := range file.Hunks {
			for _, line := range hunk.Lines {
				if v.search.pattern.MatchString(markerFor(line.Kind) + line.Text) {
					hidden++
				}
			}
		}
	}

	return hidden
}

// MatchNumber is which match the cursor is standing on, counting from one, and false when it
// is not standing on one.
func (v *View) MatchNumber(y int) (int, bool) {
	if v.search == nil {
		return 0, false
	}

	index := slices.Index(v.search.rows, y)
	if index < 0 {
		return 0, false
	}

	return index + 1, true
}

// JumpMatch is the next or previous line the pattern was found on, wrapping at the ends.
func (v *View) JumpMatch(y, direction int) (int, bool) {
	if v.search == nil || len(v.search.rows) == 0 {
		return y, false
	}

	rows := v.search.rows
	if direction > 0 {
		for _, candidate := range rows {
			if candidate > y {
				return candidate, true
			}
		}

		return rows[0], true
	}

	for index := len(rows) - 1; index >= 0; index-- {
		if rows[index] < y {
			return rows[index], true
		}
	}

	return rows[len(rows)-1], true
}

// matchText is what a row offers to a search, which for a line is the text as drawn with its
// marker and for a file header its path.
func (v *View) matchText(y int) (string, bool) {
	current, ok := v.getRow(y)
	if !ok {
		return "", false
	}

	switch current.kind {
	case fileRow:
		return current.text, true
	case threadRow:
		return FormatThreadLocation(current.thread) + " " + firstLine(current.thread), true
	case metadataRow, hunkRow, lineRow:
	}

	return v.quoteAt(y)
}

// quoteAt is a line as the diff draws it, which is what a thread quotes when it is opened.
func (v *View) quoteAt(y int) (string, bool) {
	current, ok := v.getRow(y)
	if !ok || current.kind != lineRow {
		return "", false
	}

	line := v.files[current.file].Hunks[current.hunk].Lines[current.line]

	return markerFor(line.Kind) + line.Text, true
}

// rawMatches is where the pattern was found in a row's text, measured against the text the
// search was run over.
func (v *View) rawMatches(y int) []span {
	if v.search == nil {
		return nil
	}

	return v.search.matches[y]
}

// matchesIn is where the pattern was found in a row's content, with the marker's width taken
// off.
func (v *View) matchesIn(y int) []span {
	found := v.rawMatches(y)
	if len(found) == 0 {
		return nil
	}

	shifted := make([]span, 0, len(found))
	for _, at := range found {
		// A match on the marker alone has nothing to paint: the marker is drawn in the gutter.
		if shrunk := (span{start: max(at.start-1, 0), end: max(at.end-1, 0)}); shrunk.end > shrunk.start {
			shifted = append(shifted, shrunk)
		}
	}

	return shifted
}
