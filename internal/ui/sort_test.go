package ui

import (
	"cmp"
	"strings"
	"testing"

	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
)

// sortFixture is one row of a list under test: a name and a value the row may not carry.
type sortFixture struct {
	name  string
	value int
	held  bool
}

func sortFixtures() []sortFixture {
	return []sortFixture{
		{
			name:  "d",
			value: 2,
			held:  true,
		},
		{name: "b", held: false},
		{
			name:  "c",
			value: 1,
			held:  true,
		},
		{
			name:  "e",
			value: 2,
			held:  true,
		},
		{name: "a", held: false},
		{
			name:  "f",
			value: 3,
			held:  true,
		},
	}
}

func sortFixtureColumns() []sortColumn[sortFixture] {
	return []sortColumn[sortFixture]{
		{
			id:    "value",
			label: "value",
			has: func(row sortFixture) bool {
				return row.held
			},
			compare: func(a, b sortFixture) int {
				return cmp.Compare(a.value, b.value)
			},
		},
		{
			id:    "name",
			label: "name",
			compare: func(a, b sortFixture) int {
				return strings.Compare(a.name, b.name)
			},
		},
	}
}

func sortFixtureNames(rows []sortFixture) []string {
	return xslice.Map(rows, func(row sortFixture) string { return row.name })
}

// TestSortRowsOrdersByTheColumn covers the rows without the value sitting at the bottom
// whichever way the arrow points, the comparator reversed for the down arrow, equal values
// and the bottom rows both falling to the tiebreak, and a column every row carries.
func TestSortRowsOrdersByTheColumn(t *testing.T) {
	tests := []struct {
		name  string
		order sortOrder
		want  []string
	}{
		{
			name:  "down puts the largest first and the rows without a value last",
			order: sortOrder{column: "value", direction: tabDown},
			want:  []string{"f", "d", "e", "c", "a", "b"},
		},
		{
			name:  "up puts the smallest first and the rows without a value still last",
			order: sortOrder{column: "value", direction: tabUp},
			want:  []string{"c", "d", "e", "f", "a", "b"},
		},
		{
			name:  "a column every row carries reverses whole",
			order: sortOrder{column: "name", direction: tabDown},
			want:  []string{"f", "e", "d", "c", "b", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := sortFixtures()

			sortRows(sortRowsParams[sortFixture]{
				rows:    rows,
				columns: sortFixtureColumns(),
				order:   tt.order,
				tiebreak: func(a, b sortFixture) int {
					return strings.Compare(a.name, b.name)
				},
			})

			assert.Equal(t, tt.want, sortFixtureNames(rows))
		})
	}
}

// TestSortRowsIsStable covers rows the column and the tiebreak both leave equal keeping the
// order they came in, in either direction.
func TestSortRowsIsStable(t *testing.T) {
	tests := []struct {
		name      string
		direction tabDirection
	}{
		{
			name:      "down",
			direction: tabDown,
		},
		{
			name:      "up",
			direction: tabUp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := []sortFixture{
				{
					name:  "second",
					value: 1,
					held:  true,
				},
				{
					name:  "first",
					value: 1,
					held:  true,
				},
				{name: "unheld-second"},
				{name: "unheld-first"},
			}

			sortRows(sortRowsParams[sortFixture]{
				rows:    rows,
				columns: sortFixtureColumns(),
				order:   sortOrder{column: "value", direction: tt.direction},
				tiebreak: func(_, _ sortFixture) int {
					return 0
				},
			})

			assert.Equal(t, []string{"second", "first", "unheld-second", "unheld-first"}, sortFixtureNames(rows))
		})
	}
}

// TestTabOptionsOfKeepsTheTable covers the strip's tabs coming out of the column table in its
// order, with its ids and labels.
func TestTabOptionsOfKeepsTheTable(t *testing.T) {
	assert.Equal(t, []tabOption{
		{id: "value", label: "value"},
		{id: "name", label: "name"},
	}, tabOptionsOf(sortFixtureColumns()))
}

// TestStripOrderReadsTheState covers the order naming the chosen column and its direction, and
// a chosen id the strip does not hold falling back to the strip's fallback.
func TestStripOrderReadsTheState(t *testing.T) {
	tests := []struct {
		name  string
		state tabsState
		want  sortOrder
	}{
		{
			name:  "the zero state is the fallback, down",
			state: tabsState{},
			want:  sortOrder{column: "size", direction: tabDown},
		},
		{
			name:  "a choice the strip does not hold falls back and keeps its direction",
			state: tabsState{chosen: "gone", direction: tabUp},
			want:  sortOrder{column: "size", direction: tabUp},
		},
		{
			name:  "a choice the strip holds is named as chosen",
			state: tabsState{chosen: "age", direction: tabUp},
			want:  sortOrder{column: "age", direction: tabUp},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripFixture().order(tt.state))
		})
	}
}

// TestEveryStripIsBuiltFromItsTable covers each screen's tabs being its column table and its
// fallback one of them, so escape lands on a column the rows can be ordered by.
func TestEveryStripIsBuiltFromItsTable(t *testing.T) {
	tests := []struct {
		name  string
		strip tabStrip
		table []tabOption
	}{
		{
			name:  "the agents tree",
			strip: agentsTabs(),
			table: tabOptionsOf(agentSorts),
		},
		{
			name:  "the archive",
			strip: archiveTabs(),
			table: tabOptionsOf(archiveSorts),
		},
		{
			name:  "the worktrees",
			strip: worktreesTabs(),
			table: tabOptionsOf(worktreeSorts),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.table, tt.strip.options)

			ids := xslice.Map(tt.strip.options, func(option tabOption) string { return option.id })
			assert.Contains(t, ids, tt.strip.fallback)
		})
	}
}
