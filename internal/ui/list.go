package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// spareLines is what the pane has left once the chrome has taken its share, and it goes to zero
// and below rather than being clamped, which is how a section that has no room left is dropped
// rather than drawn past the bottom of the pane.
func (m model) spareLines(chrome ...string) int {
	if m.height <= 0 {
		return 0
	}

	lines := m.height
	for _, part := range chrome {
		lines -= strings.Count(part, "\n")
	}

	return lines
}

func renderList(rows []string, cursor, lines int) string {
	if len(rows) == 0 {
		return ""
	}

	if lines <= 0 {
		return ""
	}

	if len(rows) <= lines {
		return strings.Join(rows, "\n") + "\n"
	}

	return windowRows(rows, lines, func(visible int) int {
		return windowStart(cursor, len(rows), visible)
	}, dimStyle)
}

// windowRows is the screenful of a block taller than its pane, placed by place and carrying the
// counts of what is above and below it, which is the one copy of that arithmetic.
func windowRows(rows []string, lines int, place func(visible int) int, style lipgloss.Style) string {
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

// countLines is how many rows a rendered block takes, not counting the newline it ends with,
// which closes the last row rather than opening another.
func countLines(block string) int {
	if block == "" {
		return 0
	}

	// Only the one newline that closes the last row comes off: a block padded out with empty rows
	// ends in several, and those are rows the pane draws.
	return strings.Count(strings.TrimSuffix(block, "\n"), "\n") + 1
}

func padRows(rendered string, lines int) string {
	got := strings.Count(rendered, "\n")
	if got >= lines {
		return rendered
	}

	return rendered + strings.Repeat("\n", lines-got)
}

func windowStart(cursor, total, visible int) int {
	if visible >= total {
		return 0
	}

	return max(min(cursor-visible/2, total-visible), 0)
}

func rowAt[T any](rows []T, cursor int) (T, bool) {
	if cursor < 0 || cursor >= len(rows) {
		var zero T

		return zero, false
	}

	return rows[cursor], true
}

func stepCursor(cursor, delta, length int) (int, bool) {
	next := cursor + delta
	if next < 0 || next >= length {
		return cursor, false
	}

	return next, true
}

func landCursor(idx, length int) (int, bool) {
	if length == 0 {
		return 0, false
	}

	return clampCursor(idx, length), true
}

type rowCursor struct {
	cursor   int
	selected selection
	focus    string
}

func (c rowCursor) move(delta, length int) (rowCursor, bool) {
	next, ok := stepCursor(c.cursor, delta, length)
	if !ok {
		return c, false
	}
	c.cursor = next

	return c, true
}

func (c rowCursor) jump(idx, length int) (rowCursor, bool) {
	next, ok := landCursor(idx, length)
	if !ok {
		return c, false
	}
	c.cursor = next

	return c, true
}

// listHeader is what a titled list draws above its rows, which the screens measure their own
// chrome against before they draw it.
func (m model) listHeader(title string) string {
	return m.titleStyle().Render(title) + "\n\n" + m.filterView()
}

// headedListView draws a list under a header it is handed rather than one built from a title,
// padded out so the record and the legend under it sit at the bottom of the pane whether the
// list fills it or not.
func (m model) headedListView(header string, rows []string, cursor int, help string) string {
	lines := m.listBudget(len(rows), header, help)

	return header + padRows(renderList(rows, cursor, lines), lines) + help
}

// listBudget is the rows a list has to draw into: what the pane has left once the chrome has
// taken its share, or the whole list on a pane nothing has measured yet, which has no height to
// budget against and so bounds nothing.
func (m model) listBudget(rows int, chrome ...string) int {
	if m.height <= 0 {
		return rows
	}

	return m.spareLines(chrome...)
}
