package ui

import (
	"fmt"
	"strings"
)

// listLines is how many lines a screen's list has to draw into: the pane less the alert
// queue over it and the chrome around it. The chrome is measured from what the screen
// already rendered, since the legends differ per screen and the agents one differs by what
// the cursor is on.
//
// Zero means the panel has not been told its size yet, which the lists read as unbounded,
// since clipping against a height nothing measured is worse than overflowing.
func (m model) listLines(chrome ...string) int {
	if m.height <= 0 {
		return 0
	}

	lines := m.height - m.alertLines()
	for _, part := range chrome {
		lines -= strings.Count(part, "\n")
	}

	// A pane too short to hold its own chrome still has to show the row under the cursor,
	// so the list keeps one line whatever is above it.
	return max(lines, 1)
}

// alertLines is how much of the pane the alert queue is taking, which the list below has
// that much less room for while one is showing.
func (m model) alertLines() int {
	return strings.Count(m.alertChrome(), "\n")
}

// renderList draws rows clipped to the lines it was given, keeping the row under the cursor
// on screen. A list longer than its pane otherwise runs off the bottom and takes the key
// legend with it.
//
// The window centres on the cursor rather than sliding at an edge, which is what lets it
// keep no scroll offset: the same cursor and height always give the same window, so a
// resize or a refresh that dropped rows cannot leave it pointing away from the cursor. The
// counts standing in for the rows it cut come out of the same budget.
func renderList(rows []string, cursor, lines int) string {
	if len(rows) == 0 {
		return ""
	}

	if lines <= 0 || len(rows) <= lines {
		return strings.Join(rows, "\n") + "\n"
	}

	// Each marker costs a row, and how many are needed depends on where the window lands,
	// which depends on how many rows are left after paying for them. Rather than solve
	// that, try each count and take the first that pays for what its window wants.
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

	// A pane this short cannot say what it cut and still show the row under the cursor,
	// and showing that row is the point.
	start := windowStart(cursor, len(rows), lines)

	return strings.Join(rows[start:start+lines], "\n") + "\n"
}

// markersFor is how many counts a window needs, one for each end it has rows beyond.
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

// drawWindow renders visible rows from start, each end that has rows beyond it
// carrying the count of what is off screen there.
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

// windowStart is the first row of a window of visible rows centred on the cursor,
// pinned at the top and the bottom of the list rather than scrolling past either.
func windowStart(cursor, total, visible int) int {
	if visible >= total {
		return 0
	}

	return max(min(cursor-visible/2, total-visible), 0)
}
