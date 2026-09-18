package paint

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Unbounded is the room a list is given when the pane it is drawn into has no measured height.
const Unbounded = -1

// RenderList draws a list of rows into a pane that many lines tall, windowed around the cursor
// once there are more rows than room, and dropped whole when there is none.
func RenderList(rows []string, cursor, lines int, style lipgloss.Style) string {
	if len(rows) == 0 || lines == 0 {
		return ""
	}

	if lines < 0 || len(rows) <= lines {
		return strings.Join(rows, "\n") + "\n"
	}

	return WindowRows(rows, lines, func(visible int) int {
		return windowStart(cursor, len(rows), visible)
	}, style)
}

// WindowRows is the screenful of a block taller than its pane, placed by place and carrying the
// counts of what is above and below it.
func WindowRows(rows []string, lines int, place func(visible int) int, style lipgloss.Style) string {
	// A marker costs a row, so the widest window is tried first and gives one up at a time until
	// the counts it needs have the room to be drawn beside it.
	for markers := range 3 {
		visible := lines - markers
		if visible < 1 {
			break
		}

		start := place(visible)
		if markersFor(start, visible, len(rows)) <= markers {
			return drawWindow(rows, start, visible, style)
		}
	}

	// Nothing is said about what is hidden once every window that would say it costs a row the
	// pane has not got.
	start := place(lines)

	return strings.Join(rows[start:start+lines], "\n") + "\n"
}

// markersFor counts the rows a window gives up to say what is above and below it.
func markersFor(start, visible, total int) int {
	markers := 0
	if start > 0 {
		markers++
	}
	if start+visible < total {
		markers++
	}

	return markers
}

// drawWindow draws one screenful of rows with the counts of what falls either side of it.
func drawWindow(rows []string, start, visible int, style lipgloss.Style) string {
	end := min(start+visible, len(rows))

	var b strings.Builder
	if start > 0 {
		b.WriteString(style.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	for _, row := range rows[start:end] {
		b.WriteString(row + "\n")
	}
	if rest := len(rows) - end; rest > 0 {
		b.WriteString(style.Render(fmt.Sprintf("  ↓ %d more", rest)) + "\n")
	}

	return b.String()
}

// CountLines is how many rows a rendered block takes, not counting the newline it ends with.
func CountLines(block string) int {
	if block == "" {
		return 0
	}

	// Only the one newline that closes the last row comes off: a block padded out with empty rows
	// ends in several, and those are rows the pane draws.
	return strings.Count(strings.TrimSuffix(block, "\n"), "\n") + 1
}

// PadRows fills a block out with empty rows so it keeps its share of the pane.
func PadRows(rendered string, lines int) string {
	got := strings.Count(rendered, "\n")
	if got >= lines {
		return rendered
	}

	return rendered + strings.Repeat("\n", lines-got)
}

// windowStart is the first row to draw so the cursor sits in the middle of the window.
func windowStart(cursor, total, visible int) int {
	if visible >= total {
		return 0
	}

	return max(min(cursor-visible/2, total-visible), 0)
}
