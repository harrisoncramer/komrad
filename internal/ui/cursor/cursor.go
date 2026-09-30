// Package cursor keeps a cursor inside a list of lines, and a view of the lines around it.
package cursor

// Clamp is an index kept inside a list of length lines, the first line of an empty one.
func Clamp(i, length int) int {
	return max(min(i, length-1), 0)
}

// Step is an index moved delta lines, refused when that would leave the list.
func Step(i, delta, length int) (int, bool) {
	next := i + delta
	if next < 0 || next >= length {
		return i, false
	}

	return next, true
}

// Land is an index kept inside the list, refused when the list is empty.
func Land(i, length int) (int, bool) {
	if length == 0 {
		return 0, false
	}

	return Clamp(i, length), true
}

// Place is the line found, or the index kept inside the list when nothing was found.
func Place(found, i, length int) int {
	if found >= 0 {
		return found
	}

	return Clamp(i, length)
}

// Window is a view some rows tall over lines that each take one or more rows.
type Window struct {
	// Height is how many rows the view holds.
	Height int
	// Rows is how many rows line i takes, at least one.
	Rows func(i int) int
}

// Span is what a window draws: the first and last lines, the line the cursor is on, how many rows
// of the first line fall above the view, and the row of the view the cursor's line starts on.
type Span struct {
	First     int
	Last      int
	Cursor    int
	Cut       int
	CursorRow int
}

// KeepCursor is the view ending on end moved only as far as it takes to show the cursor's line at,
// refused for a window with no rows or no lines.
func (w Window) KeepCursor(count, at, end int) (Span, bool) {
	if count <= 0 || w.Height <= 0 {
		return Span{}, false
	}

	at = Clamp(at, count)
	floor := w.floor(count)
	end = max(Clamp(end, count), floor)

	return w.span(max(w.below(at, max(end, at)), floor), at), true
}

// KeepView is the view ending on end with the cursor's line at moved only as far as it takes to
// stay inside it, refused for a window with no rows or no lines.
func (w Window) KeepView(count, at, end int) (Span, bool) {
	if count <= 0 || w.Height <= 0 {
		return Span{}, false
	}

	end = max(Clamp(end, count), w.floor(count))
	first, total := w.above(end)
	top := first
	if total > w.Height {
		top = min(first+1, end)
	}

	return w.span(end, min(max(Clamp(at, count), top), end)), true
}

// span is the view ending on end with the cursor on at.
func (w Window) span(end, at int) Span {
	first, total := w.above(end)
	cut := max(total-w.Height, 0)

	before := 0
	for i := first; i < at; i++ {
		before += w.Rows(i)
	}

	return Span{
		First:     first,
		Last:      end,
		Cursor:    at,
		Cut:       cut,
		CursorRow: max(before-cut, 0),
	}
}

// above is the first line of a view ending on end, and the rows the lines from it to end take.
func (w Window) above(end int) (int, int) {
	first, total := end, 0
	for i := end; i >= 0 && total < w.Height; i-- {
		first, total = i, total+w.Rows(i)
	}

	return first, total
}

// below is the last line a view can end on and still show the whole of line from, no further down
// than end.
func (w Window) below(from, end int) int {
	last, rows := from, w.Rows(from)
	for last < end && rows+w.Rows(last+1) <= w.Height {
		last++
		rows += w.Rows(last)
	}

	return last
}

// floor is the first line a view can end on and still show the whole of the first line, filling as
// much of the view as that allows.
func (w Window) floor(count int) int {
	filled := 0
	for i := range count {
		filled += w.Rows(i)
		if filled > w.Height {
			return max(i-1, 0)
		}
	}

	return count - 1
}
