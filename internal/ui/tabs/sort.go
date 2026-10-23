package tabs

import (
	"cmp"
	"slices"

	"komrad/lib/xslice"
)

// Column is one column a list can be ordered by.
type Column[T any] struct {
	ID    string
	Label string
	// Has reports whether a row carries the value at all, nil on a column every row has.
	Has func(T) bool
	// Compare orders two rows that both carry the value, ascending.
	Compare func(a, b T) int
}

func (c Column[T]) isHeld(row T) bool {
	return c.Has == nil || c.Has(row)
}

// rank puts a row carrying the value before one without.
func (c Column[T]) rank(row T) int {
	if c.isHeld(row) {
		return 0
	}

	return 1
}

// compareHeld is the column's own comparison, reversed for the down arrow and silent unless
// both rows carry the value.
func (c Column[T]) compareHeld(a, b T, direction Direction) int {
	if !c.isHeld(a) || !c.isHeld(b) {
		return 0
	}

	by := c.Compare(a, b)
	if direction == Down {
		return -by
	}

	return by
}

// Order is the column a list is ordered by and which way the arrow points.
type Order struct {
	Column    string
	Direction Direction
}

// Order is the column and direction the state names.
func (s Strip) Order(state State) Order {
	return Order{
		Column:    s.Options[s.IndexOf(state)].ID,
		Direction: state.Direction,
	}
}

// OptionsOf is the strip's tabs for a column table, in its order.
func OptionsOf[T any](columns []Column[T]) []Option {
	return xslice.Map(columns, func(column Column[T]) Option {
		return Option{
			ID:    column.ID,
			Label: column.Label,
		}
	})
}

// SortParams is the rows Sort orders and the columns it orders them by.
type SortParams[T any] struct {
	Rows    []T
	Columns []Column[T]
	Order   Order
	// Tiebreak orders two rows the column cannot tell apart.
	Tiebreak func(a, b T) int
}

// Sort orders the rows in place by the named column, the rows without its value at the
// bottom whichever way the arrow points.
func Sort[T any](params SortParams[T]) {
	at := max(slices.IndexFunc(params.Columns, func(column Column[T]) bool { return column.ID == params.Order.Column }), 0)
	column := params.Columns[at]
	slices.SortStableFunc(params.Rows, func(a, b T) int {
		return cmp.Or(
			cmp.Compare(column.rank(a), column.rank(b)),
			column.compareHeld(a, b, params.Order.Direction),
			params.Tiebreak(a, b),
		)
	})
}
