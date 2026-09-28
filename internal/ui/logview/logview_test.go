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

			assert.Equal(t, tt.want, viewer.Rows(tt.width, tt.height))
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
	assert.Equal(t, []string{"line 3", "line 4", "line 5"}, viewer.Rows(20, 5))
}

// TestTailReset covers starting over, which empties the viewer.
func TestTailReset(t *testing.T) {
	t.Parallel()

	viewer := logview.NewTail(100)
	viewer.Append(numbered(1, 3))
	viewer.Reset()

	assert.Zero(t, viewer.Len())
	assert.Empty(t, viewer.Rows(20, 3))
}

// TestTailScroll covers moving the view back through older lines and forward to follow again.
func TestTailScroll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		scroll []int
		more   []string
		want   []string
	}{
		{
			name:   "back a line",
			scroll: []int{-1},
			want:   []string{"line 2", "line 3", "line 4"},
		},
		{
			name:   "past the oldest stops with the window full",
			scroll: []int{-100},
			want:   []string{"line 1", "line 2", "line 3"},
		},
		{
			name:   "forward past the newest follows again",
			scroll: []int{-2, 100},
			want:   []string{"line 3", "line 4", "line 5"},
		},
		{
			name:   "scrolled back stays on its lines as more arrive",
			scroll: []int{-1},
			more:   numbered(6, 7),
			want:   []string{"line 2", "line 3", "line 4"},
		},
		{
			name: "following shows lines as they arrive",
			more: numbered(6, 7),
			want: []string{"line 5", "line 6", "line 7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			viewer := logview.NewTail(100)
			viewer.Append(numbered(1, 5))
			for _, lines := range tt.scroll {
				viewer.Scroll(lines)
			}
			viewer.Append(tt.more)

			assert.Equal(t, tt.want, viewer.Rows(20, 3))
		})
	}
}
