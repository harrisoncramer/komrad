// Package list keeps a cursor over a list of rows, the rows selected in it, and the pane it is drawn in.
package list

import (
	"komrad/internal/ui/cursor"
)

// Cursor is the row a list's cursor stands on and the rows selected around it.
type Cursor struct {
	Row      int
	Selected Selection
}

// Move is the cursor moved delta rows, refused when that would leave a list of length rows.
func (c Cursor) Move(delta, length int) (Cursor, bool) {
	next, ok := cursor.Step(c.Row, delta, length)
	if !ok {
		return c, false
	}
	c.Row = next

	return c, true
}

// Jump is the cursor put on row idx, kept inside a list of length rows and refused on an empty one.
func (c Cursor) Jump(idx, length int) (Cursor, bool) {
	next, ok := cursor.Land(idx, length)
	if !ok {
		return c, false
	}
	c.Row = next

	return c, true
}

// Keyed is a row the cursor can be put back on by name.
type Keyed interface {
	Key() string
}

// KeyAt is the key of the row at cursor, empty off the end of the rows.
func KeyAt[T Keyed](rows []T, cursor int) string {
	row, ok := At(rows, cursor)
	if !ok {
		return ""
	}

	return row.Key()
}

// IndexOf is the index of the row with the key, -1 when no row has it.
func IndexOf[T Keyed](rows []T, key string) int {
	if key == "" {
		return -1
	}

	for i, r := range rows {
		if r.Key() == key {
			return i
		}
	}

	return -1
}

// At is the row at cursor, refused off the end of the rows.
func At[T any](rows []T, cursor int) (T, bool) {
	if cursor < 0 || cursor >= len(rows) {
		var zero T

		return zero, false
	}

	return rows[cursor], true
}
