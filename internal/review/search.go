package review

import (
	"regexp"
	"slices"
)

// span is where a match sits inside a row's text, as byte offsets into the text that was
// searched, which is the drawn line including the marker the diff put in front of it.
type span struct {
	start int
	end   int
}

// search is a pattern and the rows it matches, which is every row the cursor can rest on.
type search struct {
	pattern *regexp.Regexp

	matches map[int][]span
	// rows is every matching row in order, which is what stepping through matches walks and
	// what counts them for the prompt.
	rows []int
}

// SetPattern finds the rows a pattern matches and returns how many there are. The pattern is
// compiled by the panel's own filter, which is where the case rules and the broken-pattern
// message already live.
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

// MatchCount is how many lines the pattern was found on. Several matches on one line are one
// stop, since the cursor moves a line at a time and has nowhere finer to land.
func (v *View) MatchCount() int {
	if v.search == nil {
		return 0
	}

	return len(v.search.rows)
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

// JumpMatch is the next or previous line the pattern was found on, wrapping at the ends the
// way a search does rather than stopping at them.
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

// matchText is what a row offers to a search. A line offers itself as it is drawn, marker
// included, so `^\+` finds additions; a thread that has come adrift offers where it was
// written and what it opened with, which is all there is of it on that row.
func (v *View) matchText(y int) (string, bool) {
	if current, ok := v.getRow(y); ok && current.kind == threadRow {
		return WhereOf(current.thread) + " " + firstLine(current.thread), true
	}

	return v.quoteAt(y)
}

// quoteAt is a line as the diff draws it, which is both what a thread quotes when it is opened
// and what says whether that line still reads the way it did.
func (v *View) quoteAt(y int) (string, bool) {
	current, ok := v.getRow(y)
	if !ok || current.kind != lineRow {
		return "", false
	}

	line := v.patch.Files[current.file].Hunks[current.hunk].Lines[current.line]

	return markerFor(line.Kind) + line.Text, true
}

// matchesIn is where the pattern was found in a row's content, with the marker's width taken
// off, since the content is drawn after the gutter and the marker is drawn in it.
func (v *View) matchesIn(y int) []span {
	if v.search == nil {
		return nil
	}

	found := v.search.matches[y]
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
