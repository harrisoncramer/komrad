package ui

import (
	"cmp"
	"strings"
	"testing"

	"komrad/internal/ui/tabs"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
)

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
			order: sortOrder{column: "value", direction: tabs.Down},
			want:  []string{"f", "d", "e", "c", "a", "b"},
		},
		{
			name:  "up puts the smallest first and the rows without a value still last",
			order: sortOrder{column: "value", direction: tabs.Up},
			want:  []string{"c", "d", "e", "f", "a", "b"},
		},
		{
			name:  "a column every row carries reverses whole",
			order: sortOrder{column: "name", direction: tabs.Down},
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
		direction tabs.Direction
	}{
		{
			name:      "down",
			direction: tabs.Down,
		},
		{
			name:      "up",
			direction: tabs.Up,
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
	assert.Equal(t, []tabs.Option{
		{ID: "value", Label: "value"},
		{ID: "name", Label: "name"},
	}, tabOptionsOf(sortFixtureColumns()))
}

// TestStripOrderReadsTheState covers the order naming the chosen column and its direction, and
// a chosen id the strip does not hold falling back to the strip's fallback.
func TestStripOrderReadsTheState(t *testing.T) {
	tests := []struct {
		name  string
		state tabs.State
		want  sortOrder
	}{
		{
			name:  "the zero state is the fallback, down",
			state: tabs.State{},
			want:  sortOrder{column: "size", direction: tabs.Down},
		},
		{
			name:  "a choice the strip does not hold falls back and keeps its direction",
			state: tabs.State{Chosen: "gone", Direction: tabs.Up},
			want:  sortOrder{column: "size", direction: tabs.Up},
		},
		{
			name:  "a choice the strip holds is named as chosen",
			state: tabs.State{Chosen: "age", Direction: tabs.Up},
			want:  sortOrder{column: "age", direction: tabs.Up},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripOrder(buildStrip(), tt.state))
		})
	}
}

// TestEveryStripIsBuiltFromItsTable covers each screen's tabs being its column table and its
// fallback one of them, so escape lands on a column the rows can be ordered by.
func TestEveryStripIsBuiltFromItsTable(t *testing.T) {
	tests := []struct {
		name  string
		strip tabs.Strip
		table []tabs.Option
	}{
		{
			name:  "the agents tree",
			strip: agentsTabs(),
			table: tabOptionsOf(agentSorts),
		},
		{
			name:  "the archived agents",
			strip: archivedAgentTabs(),
			table: tabOptionsOf(archivedAgentSorts),
		},
		{
			name:  "the worktrees",
			strip: worktreesTabs(),
			table: tabOptionsOf(worktreeSorts),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.table, tt.strip.Options)

			ids := xslice.Map(tt.strip.Options, func(option tabs.Option) string { return option.ID })
			assert.Contains(t, ids, tt.strip.Fallback)
		})
	}
}

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

func buildStrip() tabs.Strip {
	return tabs.Strip{
		Title:  "f",
		Legend: "sort",
		Options: []tabs.Option{
			{ID: "size", Label: "size"},
			{ID: "age", Label: "age"},
			{ID: "name", Label: "name"},
		},
		Fallback: "size",
	}
}
