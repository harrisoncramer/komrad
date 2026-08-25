package ui

import (
	"fmt"
	"strings"

	"weave/internal/ui/layout"
)

func (m model) listLines(chrome ...string) int {
	if m.height <= 0 {
		return 0
	}

	lines := m.height - m.bannerLines()
	for _, part := range chrome {
		lines -= strings.Count(part, "\n")
	}

	return max(lines, 1)
}

func (m model) bannerLines() int {
	if m.banner == nil {
		return 0
	}

	return strings.Count(m.banner.Render(m.width), "\n")
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

func (m *model) stageDocument(markdown string) {
	m.frame.Show(layout.RegionStage, newTextContent(markdown))
}

func (m model) listView(title string, rows []string, cursor int, help string) string {
	header := m.titleStyle().Render(title) + "\n\n" + m.filterView()

	return header + renderList(rows, cursor, m.listLines(header, help)) + help
}
