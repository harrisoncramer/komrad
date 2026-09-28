package logview_test

import (
	"fmt"
	"testing"

	"komrad/internal/ui/logview"

	"github.com/stretchr/testify/assert"
)

func numbered(from, to int) []string {
	var lines []string
	for i := from; i <= to; i++ {
		lines = append(lines, fmt.Sprint("line ", i))
	}

	return lines
}

// TestTailRows covers drawing the newest lines, however many rows a line wraps to.
func TestTailRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		lines  []string
		width  int
		height int
		want   []string
	}{
		{
			name:   "nothing held",
			width:  20,
			height: 3,
		},
		{
			name:   "fewer lines than fit",
			lines:  numbered(1, 2),
			width:  20,
			height: 3,
			want:   []string{"line 1", "line 2"},
		},
		{
			name:   "more lines than fit keeps the newest",
			lines:  numbered(1, 5),
			width:  20,
			height: 3,
			want:   []string{"line 3", "line 4", "line 5"},
		},
		{
			name:   "a wrapped line counts every row it takes",
			lines:  []string{"short", "a line that wraps", "last"},
			width:  9,
			height: 3,
			want:   []string{"a line th", "at wraps", "last"},
		},
		{
			name:   "escape sequences and control characters are stripped",
			lines:  []string{"\x1b[31mred\x1b[0m\ttab\rbell\a"},
			width:  40,
			height: 3,
			want:   []string{"red tabbell"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			viewer := logview.NewTail(100)
			viewer.Append(tt.lines)

			rows, _ := viewer.Rows(tt.width, tt.height)
			assert.Equal(t, tt.want, rows)
		})
	}
}

// TestTailCeiling covers the oldest lines dropping once the viewer holds more than it keeps.
func TestTailCeiling(t *testing.T) {
	t.Parallel()

	viewer := logview.NewTail(3)
	viewer.Append(numbered(1, 3))
	viewer.Append(numbered(4, 5))

	assert.Equal(t, 3, viewer.Len())
	rows, _ := viewer.Rows(20, 5)
	assert.Equal(t, []string{"line 3", "line 4", "line 5"}, rows)
}

// TestTailReset covers starting over, which empties the viewer.
func TestTailReset(t *testing.T) {
	t.Parallel()

	viewer := logview.NewTail(100)
	viewer.Append(numbered(1, 3))
	viewer.Reset()

	assert.Zero(t, viewer.Len())
	rows, _ := viewer.Rows(20, 3)
	assert.Empty(t, rows)
}

// TestTailMove covers moving the cursor with the view following it, scrolling the view with the
// cursor kept inside it, and following the log again from the newest.
func TestTailMove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		lines      []string
		width      int
		moves      []int
		scrolls    []int
		more       []string
		want       []string
		wantCursor int
	}{
		{
			name:       "the cursor starts on the newest line",
			want:       []string{"line 3", "line 4", "line 5"},
			wantCursor: 2,
		},
		{
			name:       "back a line moves the cursor and leaves the view",
			moves:      []int{-1},
			want:       []string{"line 3", "line 4", "line 5"},
			wantCursor: 1,
		},
		{
			name:  "back past the top of the view scrolls it",
			moves: []int{-3},
			want:  []string{"line 2", "line 3", "line 4"},
		},
		{
			name:  "past the oldest stops with the window full",
			moves: []int{-100},
			want:  []string{"line 1", "line 2", "line 3"},
		},
		{
			name:       "forward from the oldest moves at once",
			moves:      []int{-100, 1},
			want:       []string{"line 1", "line 2", "line 3"},
			wantCursor: 1,
		},
		{
			name:       "forward past the newest follows again",
			moves:      []int{-3, 100},
			more:       numbered(6, 7),
			want:       []string{"line 5", "line 6", "line 7"},
			wantCursor: 2,
		},
		{
			name:       "moved back stays on its line as more arrive",
			moves:      []int{-1},
			more:       numbered(6, 7),
			want:       []string{"line 3", "line 4", "line 5"},
			wantCursor: 1,
		},
		{
			name:       "scrolling the view back drags the cursor inside it",
			scrolls:    []int{-1},
			want:       []string{"line 2", "line 3", "line 4"},
			wantCursor: 2,
		},
		{
			name:    "scrolling the view forward drags the cursor down to its top",
			moves:   []int{-4},
			scrolls: []int{1},
			want:    []string{"line 2", "line 3", "line 4"},
		},
		{
			name:       "scrolling back to the newest follows again",
			scrolls:    []int{-100, 100},
			more:       numbered(6, 7),
			want:       []string{"line 5", "line 6", "line 7"},
			wantCursor: 2,
		},
		{
			name:  "the oldest line stays drawn under the cursor when the next one wraps past the window",
			lines: []string{"a", "bbbbbbbbb", "c"},
			width: 3,
			moves: []int{-2},
			want:  []string{"a"},
		},
		{
			name:    "scrolling back on a log that fits leaves the cursor where it was",
			lines:   numbered(1, 2),
			width:   20,
			moves:   []int{-1},
			scrolls: []int{-1},
			want:    []string{"line 1", "line 2"},
		},
		{
			name:  "the cursor stands on the first row of a wrapped line",
			lines: []string{"short", "a line that wraps", "last"},
			width: 9,
			moves: []int{-1},
			want:  []string{"a line th", "at wraps", "last"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lines, width := tt.lines, tt.width
			if lines == nil {
				lines, width = numbered(1, 5), 20
			}

			viewer := logview.NewTail(100)
			viewer.Append(lines)
			for _, move := range tt.moves {
				viewer.Move(move)
				viewer.Rows(width, 3)
			}
			for _, scroll := range tt.scrolls {
				viewer.Scroll(scroll)
				viewer.Rows(width, 3)
			}
			viewer.Append(tt.more)

			rows, cursor := viewer.Rows(width, 3)
			assert.Equal(t, tt.want, rows)
			assert.Equal(t, tt.wantCursor, cursor)
		})
	}
}
