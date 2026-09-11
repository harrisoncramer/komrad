package paint

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
)

var dim = lipgloss.NewStyle().Foreground(Muted)

func numbered(n int) []string {
	rows := make([]string, 0, n)
	for i := range n {
		rows = append(rows, fmt.Sprintf("row%d", i))
	}

	return rows
}

func lines(rendered string) []string {
	trimmed := strings.TrimSuffix(rendered, "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}

// TestRenderListFits pins that a list short enough to draw whole is not cut.
func TestRenderListFits(t *testing.T) {
	tests := []struct {
		name  string
		rows  []string
		lines int
	}{
		{
			name:  "shorter than the pane",
			rows:  numbered(3),
			lines: 10,
		},
		{
			name:  "exactly as tall as the pane",
			rows:  numbered(10),
			lines: 10,
		},
		{
			name:  "a height nothing has measured yet",
			rows:  numbered(100),
			lines: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.rows, lines(RenderList(tt.rows, 0, tt.lines, dim)))
		})
	}
}

// TestRenderListEmpty pins that a list with no rows draws nothing rather than a blank line.
func TestRenderListEmpty(t *testing.T) {
	assert.Empty(t, RenderList(nil, 0, 10, dim))
}

// TestRenderListWindow pins that a clipped list keeps the cursor's row on screen and never grows
// taller than the pane it was given.
func TestRenderListWindow(t *testing.T) {
	const total = 20

	tests := []struct {
		name   string
		cursor int
		lines  int
		want   []string
	}{
		{
			name:   "at the top the list is pinned there",
			cursor: 0,
			lines:  5,
			want:   []string{"row0", "row1", "row2", "row3", "  ↓ 16 more"},
		},
		{
			name:   "in the middle the cursor sits centred with both ends counted",
			cursor: 10,
			lines:  5,
			want:   []string{"  ↑ 9 more", "row9", "row10", "row11", "  ↓ 8 more"},
		},
		{
			name:   "at the bottom the list is pinned there",
			cursor: total - 1,
			lines:  5,
			want:   []string{"  ↑ 16 more", "row16", "row17", "row18", "row19"},
		},
		{
			name:   "a pane with room for one row spends it on the hovered row",
			cursor: 7,
			lines:  1,
			want:   []string{"row7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lines(RenderList(numbered(total), tt.cursor, tt.lines, dim))

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestRenderListKeepsCursorVisible walks the cursor down the list at every pane height.
func TestRenderListKeepsCursorVisible(t *testing.T) {
	rows := numbered(50)

	for height := 1; height <= len(rows); height++ {
		for cursor := range rows {
			drawn := lines(RenderList(rows, cursor, height, dim))

			assert.Contains(t, drawn, fmt.Sprintf("row%d", cursor))
			assert.LessOrEqual(t, len(drawn), height)
		}
	}
}

// TestWindowRowsNeverOutgrowsThePane pins the one bound every caller of the marker arithmetic
// rests on.
func TestWindowRowsNeverOutgrowsThePane(t *testing.T) {
	rows := make([]string, 12)
	for i := range rows {
		rows[i] = "row " + strconv.Itoa(i)
	}

	for lines := 1; lines <= len(rows)-1; lines++ {
		for scroll := range len(rows) + 2 {
			// Anchored to the end the way the review's own thread is, which is the placement that
			// puts a start in the middle and so asks for both counts at once.
			place := func(visible int) int {
				return len(rows) - visible - min(scroll, len(rows)-visible)
			}

			out := WindowRows(rows, lines, place, dim)

			assert.Equal(t, lines, strings.Count(out, "\n"),
				"WindowRows drew %d rows into %d, scroll=%d", strings.Count(out, "\n"), lines, scroll)
		}
	}
}

// TestPaintRow covers laying a background under a row that is already coloured.
func TestPaintRow(t *testing.T) {
	withColor(t)

	set := BackgroundSequence(SelectionBackground)
	row := "  " + lipgloss.NewStyle().Foreground(Accent).Render("api") + " " + dim.Render("1m")

	painted := PaintRow(row, SelectionBackground, 20)

	assert.True(t, strings.HasPrefix(painted, set), "the row opens under the background")
	assert.Equal(t, strings.Count(row, ansiReset), strings.Count(painted, ansiReset+set),
		"and every reset inside it sets the background again")
	assert.Equal(t, 20, lipgloss.Width(painted), "the bar is filled out to the pane")
	assert.True(t, strings.HasSuffix(painted, ansiReset), "and closes")

	assert.Equal(t, lipgloss.Width(row), lipgloss.Width(PaintRow(row, SelectionBackground, 2)),
		"a row wider than the pane is painted as it stands rather than padded")
}

// TestPaintRowWithoutColour covers a terminal that writes no escapes.
func TestPaintRowWithoutColour(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.Ascii)

	assert.Equal(t, "  api", PaintRow("  api", SelectionBackground, 20))
}
