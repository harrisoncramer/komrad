package ui

import (
	"strings"

	"weave/internal/ui/paint"
)

// listLines is the room the list has once the chrome around it has taken its share, and is zero
// when the chrome fills the pane.
func (m model) listLines(chrome ...string) int {
	if m.height <= 0 {
		return paint.Unbounded
	}

	return max(m.spareLines(chrome...), 0)
}

// spareLines is what the pane has left once the chrome has taken its share, unclamped.
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

func (m model) listHeader(title string) string {
	return m.talliedHeader(title, "")
}

// talliedHeader is a list's header with a muted tally after its title.
func (m model) talliedHeader(title, tally string) string {
	header := paint.Panel.Title(title)
	if tally != "" {
		header += dimStyle.Render("  " + tally)
	}

	return header + "\n\n" + m.filterView()
}

// headedListView draws a list under its header, padded out so the record and the legend under it
// sit at the bottom of the pane whether the list fills it or not.
func (m model) headedListView(header string, rows []string, cursor int, help string) string {
	lines := m.listLines(header, help)

	return m.headedBlockView(header, paint.RenderList(rows, cursor, lines, dimStyle), lines, help)
}

// headedBlockView draws a list already fitted to the pane.
func (m model) headedBlockView(header, block string, lines int, help string) string {
	return header + paint.PadRows(block, lines) + help
}

// keyed is a row the cursor can be put back on by name once the list has changed under it.
type keyed interface {
	key() string
}

func rowKeyAt[T keyed](rows []T, cursor int) string {
	row, ok := rowAt(rows, cursor)
	if !ok {
		return ""
	}

	return row.key()
}

func rowIndexOfKey[T keyed](rows []T, key string) int {
	if key == "" {
		return -1
	}

	for i, r := range rows {
		if r.key() == key {
			return i
		}
	}

	return -1
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
