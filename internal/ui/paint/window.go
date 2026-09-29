package paint

import (
	"fmt"
	"strings"

	"komrad/internal/glyph"

	"charm.land/lipgloss/v2"
)

// Unbounded is the room a list is given when the pane it is drawn into has no measured height.
const Unbounded = -1

// Window is the rows of a block a pane shows and whether they are marked with the counts either
// side.
type Window struct {
	Start  int
	End    int
	Marked bool
}

// RenderList draws a list of rows into a pane that many lines tall, windowed around the cursor
// once there are more rows than room, and dropped whole when there is none.
func RenderList(rows []string, cursor, lines int, style lipgloss.Style) string {
	window := WindowAround(len(rows), cursor, lines)

	return DrawWindow(rows[window.Start:window.End], window, len(rows), style)
}

// WindowAround is the rows of a block a pane that many lines tall shows, placed around the cursor.
func WindowAround(total, cursor, lines int) Window {
	return WindowOf(total, lines, func(visible int) int {
		return windowStart(cursor, total, visible)
	})
}

// WindowRows is the screenful of a block taller than its pane, placed by place and carrying the
// counts of what is above and below it.
func WindowRows(rows []string, lines int, place func(visible int) int, style lipgloss.Style) string {
	window := WindowOf(len(rows), lines, place)

	return DrawWindow(rows[window.Start:window.End], window, len(rows), style)
}

// WindowOf is the rows of a block a pane that many lines tall shows, placed by place.
func WindowOf(total, lines int, place func(visible int) int) Window {
	if total == 0 || lines == 0 {
		return Window{}
	}

	if lines < 0 || total <= lines {
		return Window{End: total}
	}

	// A marker costs a row, so the widest window is tried first and gives one up at a time until
	// the counts it needs have the room to be drawn beside it.
	for markers := range 3 {
		visible := lines - markers
		if visible < 1 {
			break
		}

		start := place(visible)
		if markersFor(start, visible, total) <= markers {
			return Window{
				Start:  start,
				End:    min(start+visible, total),
				Marked: true,
			}
		}
	}

	// Nothing is said about what is hidden once every window that would say it costs a row the
	// pane has not got.
	start := place(lines)

	return Window{Start: start, End: start + lines}
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

// DrawWindow draws the rows a window shows and the counts it is marked with.
func DrawWindow(rows []string, window Window, total int, style lipgloss.Style) string {
	if len(rows) == 0 {
		return ""
	}

	var b strings.Builder
	if window.Marked && window.Start > 0 {
		b.WriteString(MoreAbove(window.Start, style) + "\n")
	}
	for _, row := range rows {
		b.WriteString(row + "\n")
	}
	if rest := total - window.End; window.Marked && rest > 0 {
		b.WriteString(MoreBelow(rest, style) + "\n")
	}

	return b.String()
}

// MoreAbove is the marker over a window saying how many rows it scrolled past.
func MoreAbove(count int, style lipgloss.Style) string {
	return style.Render(fmt.Sprintf("  %s %d more", glyph.Up, count))
}

// MoreBelow is the marker under a window saying how many rows are left below it.
func MoreBelow(count int, style lipgloss.Style) string {
	return style.Render(fmt.Sprintf("  %s %d more", glyph.Down, count))
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
