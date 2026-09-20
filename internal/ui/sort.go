package ui

import (
	"cmp"
	"slices"

	"weave/lib/xslice"
)

// sortColumn is one column a list can be ordered by.
type sortColumn[T any] struct {
	id    string
	label string
	// direction is the way the arrow points when the column is stepped onto.
	direction tabDirection
	// has reports whether a row carries the value at all, nil on a column every row has.
	has func(T) bool
	// compare orders two rows that both carry the value, ascending.
	compare func(a, b T) int
}

func (c sortColumn[T]) isHeld(row T) bool {
	return c.has == nil || c.has(row)
}

// rank puts a row carrying the value before one without.
func (c sortColumn[T]) rank(row T) int {
	if c.isHeld(row) {
		return 0
	}

	return 1
}

// compareHeld is the column's own comparison, reversed for the down arrow and silent unless
// both rows carry the value.
func (c sortColumn[T]) compareHeld(a, b T, direction tabDirection) int {
	if !c.isHeld(a) || !c.isHeld(b) {
		return 0
	}

	by := c.compare(a, b)
	if direction == tabDown {
		return -by
	}

	return by
}

// sortOrder is the column a list is ordered by and which way the arrow points.
type sortOrder struct {
	column    string
	direction tabDirection
}

// order is the column and direction a strip's state names.
func (s tabStrip) order(state tabsState) sortOrder {
	return sortOrder{
		column:    s.options[s.indexOf(state)].id,
		direction: state.direction,
	}
}

func tabOptionsOf[T any](columns []sortColumn[T]) []tabOption {
	return xslice.Map(columns, func(column sortColumn[T]) tabOption {
		return tabOption{
			id:        column.id,
			label:     column.label,
			direction: column.direction,
		}
	})
}

type sortRowsParams[T any] struct {
	rows    []T
	columns []sortColumn[T]
	order   sortOrder
	// tiebreak orders two rows the column cannot tell apart.
	tiebreak func(a, b T) int
}

// sortRows orders the rows in place by the named column, the rows without its value at the
// bottom whichever way the arrow points.
func sortRows[T any](params sortRowsParams[T]) {
	at := max(slices.IndexFunc(params.columns, func(column sortColumn[T]) bool { return column.id == params.order.column }), 0)
	column := params.columns[at]
	slices.SortStableFunc(params.rows, func(a, b T) int {
		return cmp.Or(
			cmp.Compare(column.rank(a), column.rank(b)),
			column.compareHeld(a, b, params.order.direction),
			params.tiebreak(a, b),
		)
	})
}
