package ui

import (
	"fmt"
	"strings"
)

func (m model) listLines(chrome ...string) int {
	if m.height <= 0 {
		return 0
	}

	return max(m.spareLines(chrome...), 1)
}

// spareLines is what the pane has left once the chrome has taken its share, unclamped, which is
// how a section that would not fit is dropped rather than drawn past the bottom of the pane.
func (m model) spareLines(chrome ...string) int {
	if m.height <= 0 {
		return 0
	}

	lines := m.height - m.chromeLines()
	for _, part := range chrome {
		lines -= strings.Count(part, "\n")
	}

	return lines
}

// chromeLines is what the view draws above whatever screen is up, which is room that screen does
// not have.
func (m model) chromeLines() int {
	if m.banner == nil {
		return 0
	}

	return strings.Count(m.banner.Render(m.paneWidth()), "\n")
}

func renderList(rows []string, cursor, lines int) string {
	if len(rows) == 0 {
		return ""
	}

	if lines <= 0 || len(rows) <= lines {
		return strings.Join(rows, "\n") + "\n"
	}

	for markers := range 3 {
		visible := lines - markers
		if visible < 1 {
			break
		}

		start := windowStart(cursor, len(rows), visible)
		if markersFor(start, visible, len(rows)) <= markers {
			return drawWindow(rows, start, visible)
		}
	}

	start := windowStart(cursor, len(rows), lines)

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

func drawWindow(rows []string, start, visible int) string {
	end := min(start+visible, len(rows))

	var b strings.Builder
	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	for _, row := range rows[start:end] {
		b.WriteString(row + "\n")
	}
	if rest := len(rows) - end; rest > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more", rest)) + "\n")
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

// tailRows cuts a block down to what the pane can hold, keeping the end, since bubbletea draws
// whatever it is handed and a view taller than the pane scrolls the pane away.
func tailRows(rendered string, lines int) string {
	if lines <= 0 {
		return ""
	}

	rows := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(rows) <= lines {
		return rendered
	}

	kept := rows[len(rows)-(lines-1):]
	marker := dimStyle.Render(fmt.Sprintf("  ↑ %d more", len(rows)-len(kept))) + "\n"

	// Joining an empty tail onto the marker still writes a newline, which is one row more than
	// the pane has.
	if len(kept) == 0 {
		return marker
	}

	return marker + strings.Join(kept, "\n") + "\n"
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

func (m model) listView(title string, rows []string, cursor int, help string) string {
	header := m.titleStyle().Render(title) + "\n\n" + m.filterView()

	return header + renderList(rows, cursor, m.listLines(header, help)) + help
}
