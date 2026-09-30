package cursor_test

import (
	"testing"

	"komrad/internal/ui/cursor"

	"github.com/stretchr/testify/assert"
)

// TestClamp covers an index kept inside a list, an empty one included.
func TestClamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		i      int
		length int
		want   int
	}{
		{
			name:   "an empty list pins to zero",
			i:      0,
			length: 0,
			want:   0,
		},
		{
			name:   "a negative index pins to the first line",
			i:      -1,
			length: 3,
			want:   0,
		},
		{
			name:   "an index past the end pins to the last line",
			i:      9,
			length: 3,
			want:   2,
		},
		{
			name:   "an index inside the list is left alone",
			i:      2,
			length: 5,
			want:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, cursor.Clamp(tt.i, tt.length))
		})
	}
}

// TestStep covers a step inside the list and the steps refused at either end.
func TestStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		i      int
		delta  int
		length int
		want   int
		wantOK bool
	}{
		{
			name:   "a step inside the list moves",
			i:      1,
			delta:  1,
			length: 3,
			want:   2,
			wantOK: true,
		},
		{
			name:   "a step past the last line is refused",
			i:      2,
			delta:  1,
			length: 3,
			want:   2,
		},
		{
			name:   "a step before the first line is refused",
			i:      0,
			delta:  -1,
			length: 3,
			want:   0,
		},
		{
			name:   "a step in an empty list is refused",
			i:      0,
			delta:  1,
			length: 0,
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := cursor.Step(tt.i, tt.delta, tt.length)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

// TestLand covers landing on a line, and an empty list refusing it.
func TestLand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		i      int
		length int
		want   int
		wantOK bool
	}{
		{
			name:   "an empty list is refused",
			i:      2,
			length: 0,
			want:   0,
		},
		{
			name:   "an index past the end lands on the last line",
			i:      9,
			length: 3,
			want:   2,
			wantOK: true,
		},
		{
			name:   "an index inside the list lands on it",
			i:      1,
			length: 3,
			want:   1,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := cursor.Land(tt.i, tt.length)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

// TestPlace covers the line found winning over the index it would otherwise keep.
func TestPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		found  int
		i      int
		length int
		want   int
	}{
		{
			name:   "the line found wins",
			found:  1,
			i:      4,
			length: 5,
			want:   1,
		},
		{
			name:   "nothing found keeps the index inside the list",
			found:  -1,
			i:      9,
			length: 3,
			want:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, cursor.Place(tt.found, tt.i, tt.length))
		})
	}
}

// TestWindow covers the view moving to show the cursor, the cursor moving to stay in the view,
// and lines that wrap to more than one row.
func TestWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// rows is how many rows each line takes, five lines of one row each when unset.
		rows   []int
		height int
		// view says the view moved last, so the cursor is kept inside it.
		view   bool
		at     int
		end    int
		want   cursor.Span
		wantOK bool
	}{
		{
			name: "a cursor inside the view leaves it where it is",
			at:   3,
			end:  4,
			want: cursor.Span{
				First:     2,
				Last:      4,
				Cursor:    3,
				CursorRow: 1,
			},
			wantOK: true,
		},
		{
			name: "a cursor above the view brings the view up to it",
			at:   1,
			end:  4,
			want: cursor.Span{
				First:  1,
				Last:   3,
				Cursor: 1,
			},
			wantOK: true,
		},
		{
			name: "a cursor below the view brings the view down to it",
			at:   4,
			end:  2,
			want: cursor.Span{
				First:     2,
				Last:      4,
				Cursor:    4,
				CursorRow: 2,
			},
			wantOK: true,
		},
		{
			name: "a cursor past the oldest stops on it with the view full",
			at:   -10,
			end:  4,
			want: cursor.Span{
				First:  0,
				Last:   2,
				Cursor: 0,
			},
			wantOK: true,
		},
		{
			name: "the oldest line stays whole when the next one wraps past the view",
			rows: []int{1, 3, 1},
			at:   0,
			end:  2,
			want: cursor.Span{
				First:  0,
				Last:   0,
				Cursor: 0,
			},
			wantOK: true,
		},
		{
			name: "the cursor's row is the first row of a wrapped line",
			rows: []int{1, 2, 1},
			at:   1,
			end:  2,
			want: cursor.Span{
				First:  1,
				Last:   2,
				Cursor: 1,
			},
			wantOK: true,
		},
		{
			name: "rows cut off above the view come off the cursor's row",
			rows: []int{2, 2, 2},
			at:   2,
			end:  2,
			want: cursor.Span{
				First:     1,
				Last:      2,
				Cursor:    2,
				Cut:       1,
				CursorRow: 1,
			},
			wantOK: true,
		},
		{
			name: "a view moved above the cursor drags it up to its last line",
			view: true,
			at:   4,
			end:  3,
			want: cursor.Span{
				First:     1,
				Last:      3,
				Cursor:    3,
				CursorRow: 2,
			},
			wantOK: true,
		},
		{
			name: "a view moved below the cursor drags it down to its first line",
			view: true,
			at:   0,
			end:  4,
			want: cursor.Span{
				First:  2,
				Last:   4,
				Cursor: 2,
			},
			wantOK: true,
		},
		{
			name: "a view holding every line leaves the cursor where it is",
			rows: []int{1, 1},
			view: true,
			at:   0,
			end:  1,
			want: cursor.Span{
				First:  0,
				Last:   1,
				Cursor: 0,
			},
			wantOK: true,
		},
		{
			name: "a view never leaves the cursor on a line cut off above it",
			rows: []int{2, 2, 2},
			view: true,
			at:   0,
			end:  2,
			want: cursor.Span{
				First:     1,
				Last:      2,
				Cursor:    2,
				Cut:       1,
				CursorRow: 1,
			},
			wantOK: true,
		},
		{
			name: "no lines is refused",
			rows: []int{},
		},
		{
			name:   "no rows is refused",
			height: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows, height := tt.rows, tt.height
			if rows == nil {
				rows = []int{1, 1, 1, 1, 1}
			}
			if height == 0 {
				height = 3
			}

			window := cursor.Window{Height: height, Rows: func(i int) int { return rows[i] }}
			keep := window.KeepCursor
			if tt.view {
				keep = window.KeepView
			}

			got, ok := keep(len(rows), tt.at, tt.end)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}
