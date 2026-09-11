package ui

import (
	"strings"

	"weave/internal/ui/paint"
)

func (m model) listLines(chrome ...string) int {
	if m.height <= 0 {
		return 0
	}

	return max(m.spareLines(chrome...), 1)
}

// spareLines is what the pane has left once the chrome has taken its share, unclamped, so a
// section that would not fit is dropped rather than drawn past the bottom of the pane.
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
	return m.headedListView(paint.TitleOf(m.isListFocused(), title)+"\n\n"+m.filterView(), rows, cursor, help)
}

// headedListView draws a list under a header it is handed rather than one built from a title,
// padded out so the record and the legend under it sit at the bottom of the pane whether the
// list fills it or not.
func (m model) headedListView(header string, rows []string, cursor int, help string) string {
	lines := m.listLines(header, help)

	return header + paint.PadRows(paint.RenderList(rows, cursor, lines, dimStyle), lines) + help
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

func clampCursor(idx, length int) int {
	return max(min(idx, length-1), 0)
}
