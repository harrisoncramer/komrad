package ui

import (
	"fmt"
	"strings"

	"weave/internal/ui/layout"
)

// listLines is how many lines a screen's list has to draw into: the pane less the banner
// over it and the chrome around it. The chrome is measured from what the screen already
// rendered, since the legends differ per screen and the agents one differs by what the
// cursor is on.
//
// Zero means the panel has not been told its size yet, which the lists read as unbounded,
// since clipping against a height nothing measured is worse than overflowing.
func (m model) listLines(chrome ...string) int {
	if m.height <= 0 {
		return 0
	}

	lines := m.height - m.bannerLines()
	for _, part := range chrome {
		lines -= strings.Count(part, "\n")
	}

	// A pane too short to hold its own chrome still has to show the row under the cursor,
	// so the list keeps one line whatever is above it.
	return max(lines, 1)
}

// bannerLines is how much of the pane a refused write is taking, which the list below has
// that much less room for while one is up.
func (m model) bannerLines() int {
	if m.banner == nil {
		return 0
	}

	return strings.Count(m.banner.Render(m.width), "\n")
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

// rowCursor is the cursor, selection, and pending focus a screen walking agentRow rows
// keeps. The tree and the archive each embed one, since both are a cursor over the same
// row shape.
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

// stageDocument pages a rendered document in the stage, which is what a screen shows for a
// row with no pane of its own.
func (m *model) stageDocument(markdown string) {
	m.frame.Show(layout.RegionStage, newTextContent(markdown))
}

func (m model) listView(title string, rows []string, cursor int, help string) string {
	header := m.titleStyle().Render(title) + "\n\n" + m.filterView()

	return header + renderList(rows, cursor, m.listLines(header, help)) + help
}
