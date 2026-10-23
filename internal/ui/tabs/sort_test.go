package tabs_test

import (
	"cmp"
	"strings"
	"testing"

	"komrad/internal/ui/tabs"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
)

// TestSortOrdersByTheColumn covers the rows without the value sitting at the bottom
// whichever way the arrow points, the comparator reversed for the down arrow, equal values
// and the bottom rows both falling to the tiebreak, and a column every row carries.
func TestSortOrdersByTheColumn(t *testing.T) {
	tests := []struct {
		name  string
		order tabs.Order
		want  []string
	}{
		{
			name:  "down puts the largest first and the rows without a value last",
			order: tabs.Order{Column: "value", Direction: tabs.Down},
			want:  []string{"f", "d", "e", "c", "a", "b"},
		},
		{
			name:  "up puts the smallest first and the rows without a value still last",
			order: tabs.Order{Column: "value", Direction: tabs.Up},
			want:  []string{"c", "d", "e", "f", "a", "b"},
		},
		{
			name:  "a column every row carries reverses whole",
			order: tabs.Order{Column: "name", Direction: tabs.Down},
			want:  []string{"f", "e", "d", "c", "b", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := sortFixtures()

			tabs.Sort(tabs.SortParams[sortFixture]{
				Rows:    rows,
				Columns: sortFixtureColumns(),
				Order:   tt.order,
				Tiebreak: func(a, b sortFixture) int {
					return strings.Compare(a.name, b.name)
				},
			})

			assert.Equal(t, tt.want, sortFixtureNames(rows))
		})
	}
}

// TestSortIsStable covers rows the column and the tiebreak both leave equal keeping the
// order they came in, in either direction.
func TestSortIsStable(t *testing.T) {
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

			tabs.Sort(tabs.SortParams[sortFixture]{
				Rows:    rows,
				Columns: sortFixtureColumns(),
				Order:   tabs.Order{Column: "value", Direction: tt.direction},
				Tiebreak: func(_, _ sortFixture) int {
					return 0
				},
			})

			assert.Equal(t, []string{"second", "first", "unheld-second", "unheld-first"}, sortFixtureNames(rows))
		})
	}
}

// TestOptionsOfKeepsTheTable covers the strip's tabs coming out of the column table in its
// order, with its ids and labels.
func TestOptionsOfKeepsTheTable(t *testing.T) {
	assert.Equal(t, []tabs.Option{
		{ID: "value", Label: "value"},
		{ID: "name", Label: "name"},
	}, tabs.OptionsOf(sortFixtureColumns()))
}

// TestOrderReadsTheState covers the order naming the chosen column and its direction, and
// a chosen id the strip does not hold falling back to the strip's fallback.
func TestOrderReadsTheState(t *testing.T) {
	tests := []struct {
		name  string
		state tabs.State
		want  tabs.Order
	}{
		{
			name:  "the zero state is the fallback, down",
			state: tabs.State{},
			want:  tabs.Order{Column: "size", Direction: tabs.Down},
		},
		{
			name:  "a choice the strip does not hold falls back and keeps its direction",
			state: tabs.State{ChosenColumn: "gone", Direction: tabs.Up},
			want:  tabs.Order{Column: "size", Direction: tabs.Up},
		},
		{
			name:  "a choice the strip holds is named as chosen",
			state: tabs.State{ChosenColumn: "age", Direction: tabs.Up},
			want:  tabs.Order{Column: "age", Direction: tabs.Up},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildStrip().Order(tt.state))
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

func sortFixtureColumns() []tabs.Column[sortFixture] {
	return []tabs.Column[sortFixture]{
		{
			ID:    "value",
			Label: "value",
			Has: func(row sortFixture) bool {
				return row.held
			},
			Compare: func(a, b sortFixture) int {
				return cmp.Compare(a.value, b.value)
			},
		},
		{
			ID:    "name",
			Label: "name",
			Compare: func(a, b sortFixture) int {
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
