// Package reader walks a cursor through rows already rendered: the window that draws it, and the
// rows a search found.
package reader

import (
	"regexp"
	"slices"

	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/xslice"
)

// Window is the rows of a document a view of some height draws, counting off what is past either
// end when it has markers.
type Window struct {
	Len     int
	Top     int
	Markers bool
}

// Clamp keeps a first row inside the document.
func (w Window) Clamp(top, height int) int {
	if w.Len <= height {
		return 0
	}

	// The marker saying what is above takes a row of its own once the reader has scrolled, so a
	// document that counts them ends one row further down than the window is tall.
	end := w.Len - height
	if w.Markers {
		end++
	}

	return max(min(top, end), 0)
}

// Span is the rows the window draws, from the first to one past the last.
func (w Window) Span(height int) (int, int) {
	top := w.Clamp(w.Top, height)

	above := 0
	if w.Markers && top > 0 {
		above = 1
	}

	// The marker under the text takes a row as well.
	end := min(top+max(height-above, 0), w.Len)
	if w.Markers && end < w.Len {
		end = max(end-1, top)
	}

	return top, end
}

// Reveal is the first row of the window moved as little as it takes to draw a row.
func (w Window) Reveal(row, height int) int {
	w.Top = w.Clamp(w.Top, height)
	for {
		first, end := w.Span(height)
		switch {
		case row < first:
			return w.Clamp(row, height)
		case row < end:
			return first
		}

		next := w.Clamp(w.Top+1, height)
		if next == w.Top {
			return w.Top
		}
		w.Top = next
	}
}

// Reader is a cursor on one row of a document and the first row its window draws.
type Reader struct {
	Line int
	Top  int
}

func (r Reader) window(rows int) Window {
	return Window{Len: rows, Top: r.Top, Markers: true}
}

// MoveTo puts the cursor on a row and brings the window round to it.
func (r *Reader) MoveTo(line, rows, height int) {
	r.Line = cursor.Clamp(line, rows)
	r.Top = r.window(rows).Reveal(r.Line, height)
}

// Scroll moves the window and leaves the cursor where it is.
func (r *Reader) Scroll(delta, rows, height int) {
	window := r.window(rows)
	r.Top = window.Clamp(window.Clamp(r.Top, height)+delta, height)
}

// Settle keeps the cursor where it is while the window draws it, and puts it on the first row
// drawn otherwise.
func (r *Reader) Settle(rows, height int) {
	first, end := r.window(rows).Span(height)
	if r.Line < first || r.Line >= end {
		r.Line = first
	}
}

// Matches is every row a pattern is found on in its plain text, in order.
func Matches(pattern *regexp.Regexp, rows []string) []int {
	if pattern == nil {
		return nil
	}

	var found []int
	for i, row := range rows {
		if pattern.MatchString(paint.PlainText(row)) {
			found = append(found, i)
		}
	}

	return found
}

// Spans is where a pattern is found in a row's plain text.
func Spans(pattern *regexp.Regexp, row string) []paint.Span {
	if pattern == nil {
		return nil
	}

	return xslice.Map(pattern.FindAllStringIndex(paint.PlainText(row), -1), func(at []int) paint.Span {
		return paint.Span{Start: at[0], End: at[1]}
	})
}

// Jump is the next matching row after one for a positive direction and the previous before it
// otherwise, wrapping at the ends, and false when nothing matched.
func Jump(found []int, from, direction int) (int, bool) {
	if len(found) == 0 {
		return from, false
	}

	if direction > 0 {
		for _, row := range found {
			if row > from {
				return row, true
			}
		}

		return found[0], true
	}

	for i := len(found) - 1; i >= 0; i-- {
		if found[i] < from {
			return found[i], true
		}
	}

	return found[len(found)-1], true
}

// From is the first matching row at or after one, wrapping to the first, and false when nothing
// matched.
func From(found []int, from int) (int, bool) {
	if len(found) == 0 {
		return from, false
	}

	at, _ := slices.BinarySearch(found, from)

	return found[at%len(found)], true
}

// Count is where among the matches a row stands, or how many there are when it is not one.
func Count(found []int, at int) string {
	if i := slices.Index(found, at); i >= 0 {
		return label.OfCount(i+1, len(found))
	}

	return label.Counted(len(found), "match")
}
