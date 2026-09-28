// Package logview holds log lines and draws the newest of them, or the ones around a cursor.
package logview

import (
	"strings"

	"komrad/lib/xslice"

	"github.com/charmbracelet/x/ansi"
)

// LogViewer holds log lines and draws the newest of them, or the ones around a cursor moved back.
type LogViewer interface {
	// Append adds lines after the last, dropping the oldest past the ceiling.
	Append(lines []string)
	// Reset drops every line.
	Reset()
	// Len is how many lines are held.
	Len() int
	// Move moves the cursor a number of lines toward the newest for a positive count and toward
	// the oldest for a negative one, stopping at either end, and the view follows it.
	Move(lines int)
	// Scroll moves the view a number of lines toward the newest for a positive count and toward
	// the oldest for a negative one, stopping at either end, and the cursor is kept inside it.
	Scroll(lines int)
	// Rows is the lines wrapped to width, as many rows of them as fit in height with the cursor's
	// line among them, and the row the cursor's line starts on.
	Rows(width, height int) ([]string, int)
}

type tail struct {
	lines   []string
	ceiling int
	// cursor is how many lines the cursor stands before the newest, zero while it follows the log.
	cursor int
	// back is how many lines the view ends before the newest.
	back int
	// scrolled says the view moved last, so the cursor is kept inside it rather than it around the
	// cursor.
	scrolled bool
}

// NewTail is a LogViewer that keeps at most ceiling lines.
func NewTail(ceiling int) LogViewer {
	return &tail{ceiling: ceiling}
}

func (t *tail) Append(lines []string) {
	t.lines = append(t.lines, xslice.Map(lines, sanitize)...)
	switch {
	case t.cursor > 0:
		t.cursor += len(lines)
		t.back += len(lines)
	case t.back > 0:
		t.back += len(lines)
	}

	if over := len(t.lines) - t.ceiling; over > 0 {
		t.lines = append([]string(nil), t.lines[over:]...)
	}
	t.cursor, t.back = t.clamp(t.cursor), t.clamp(t.back)
}

func (t *tail) Reset() {
	t.lines, t.cursor, t.back, t.scrolled = nil, 0, 0, false
}

func (t *tail) Len() int {
	return len(t.lines)
}

func (t *tail) Move(lines int) {
	t.cursor = t.clamp(t.cursor - lines)
	t.scrolled = false
}

func (t *tail) Scroll(lines int) {
	t.back = t.clamp(t.back - lines)
	t.scrolled = true
}

// clamp is a count of lines before the newest kept to the lines held.
func (t *tail) clamp(back int) int {
	return min(max(back, 0), max(len(t.lines)-1, 0))
}

func (t *tail) Rows(width, height int) ([]string, int) {
	if height <= 0 || len(t.lines) == 0 {
		return nil, -1
	}

	newest := len(t.lines) - 1
	end := max(newest-t.back, t.firstFilling(width, height))
	if !t.scrolled {
		at := newest - t.cursor
		end = max(t.lastBelow(at, max(end, at), width, height), t.firstFilling(width, height))
	}
	t.back = newest - end

	var blocks [][]string
	first, total := end, 0
	for i := end; i >= 0 && total < height; i-- {
		blocks = append(blocks, t.wrap(i, width))
		first, total = i, total+len(blocks[len(blocks)-1])
	}

	if t.scrolled {
		top := first
		if total > height {
			top = min(first+1, end)
		}
		t.cursor = newest - min(max(newest-t.cursor, top), end)
		if t.back == 0 {
			t.cursor = 0
		}
	}

	at := newest - t.cursor
	var rows []string
	cursorRow := 0
	for k := len(blocks) - 1; k >= 0; k-- {
		if first+len(blocks)-1-k == at {
			cursorRow = len(rows)
		}
		rows = append(rows, blocks[k]...)
	}

	cut := max(len(rows)-height, 0)

	return rows[cut:], max(cursorRow-cut, 0)
}

// lastBelow is the line the view ends on to keep the cursor's line at from in sight, no further
// down than end.
func (t *tail) lastBelow(from, end, width, height int) int {
	last, rows := from, len(t.wrap(from, width))
	for last < end && rows+len(t.wrap(last+1, width)) <= height {
		last++
		rows += len(t.wrap(last, width))
	}

	return last
}

// firstFilling is the first line the view can end on and still fill height rows.
func (t *tail) firstFilling(width, height int) int {
	filled := 0
	for i := range t.lines {
		filled += len(t.wrap(i, width))
		if filled >= height {
			return i
		}
	}

	return len(t.lines) - 1
}

// wrap is one held line broken into the rows it takes at width.
func (t *tail) wrap(i, width int) []string {
	return strings.Split(ansi.Hardwrap(t.lines[i], max(width, 1), true), "\n")
}

// sanitize strips escape sequences and control characters from a line the panel draws itself.
func sanitize(line string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < ' ' || r == 0x7f:
			return -1
		default:
			return r
		}
	}, ansi.Strip(line))
}
