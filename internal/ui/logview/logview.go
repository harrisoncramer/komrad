// Package logview holds log lines and draws the newest of them, or the ones around a cursor.
package logview

import (
	"strings"

	"komrad/internal/ui/cursor"
	"komrad/lib/xslice"

	"github.com/charmbracelet/x/ansi"
)

// Tail holds log lines and draws the newest of them, or the ones around a cursor moved back.
type Tail struct {
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

// NewTail is a Tail that keeps at most ceiling lines.
func NewTail(ceiling int) *Tail {
	return &Tail{ceiling: ceiling}
}

// Append adds lines after the last, dropping the oldest past the ceiling.
func (t *Tail) Append(lines []string) {
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
	t.cursor, t.back = cursor.Clamp(t.cursor, len(t.lines)), cursor.Clamp(t.back, len(t.lines))
}

// Reset drops every line.
func (t *Tail) Reset() {
	t.lines, t.cursor, t.back, t.scrolled = nil, 0, 0, false
}

// Len is how many lines are held.
func (t *Tail) Len() int {
	return len(t.lines)
}

// Move moves the cursor a number of lines toward the newest for a positive count and toward
// the oldest for a negative one, stopping at either end, and the view follows it.
func (t *Tail) Move(lines int) {
	t.cursor = cursor.Clamp(t.cursor-lines, len(t.lines))
	t.scrolled = false
}

// Scroll moves the view a number of lines toward the newest for a positive count and toward
// the oldest for a negative one, stopping at either end, and the cursor is kept inside it.
func (t *Tail) Scroll(lines int) {
	t.back = cursor.Clamp(t.back-lines, len(t.lines))
	t.scrolled = true
	if lines > 0 && t.back == 0 {
		t.cursor = 0
	}
}

// Rows is the lines wrapped to width, as many rows of them as fit in height with the cursor's
// line among them, and the row the cursor's line starts on.
func (t *Tail) Rows(width, height int) ([]string, int) {
	wrapped := make(map[int][]string)
	wrap := func(i int) []string {
		if _, ok := wrapped[i]; !ok {
			wrapped[i] = t.wrap(i, width)
		}

		return wrapped[i]
	}

	window := cursor.Window{Height: height, Rows: func(i int) int { return len(wrap(i)) }}
	keep := window.KeepCursor
	if t.scrolled {
		keep = window.KeepView
	}

	newest := len(t.lines) - 1
	span, ok := keep(len(t.lines), newest-t.cursor, newest-t.back)
	if !ok {
		return nil, -1
	}
	t.cursor, t.back = newest-span.Cursor, newest-span.Last

	var rows []string
	for i := span.First; i <= span.Last; i++ {
		rows = append(rows, wrap(i)...)
	}

	return rows[span.Cut:], span.CursorRow
}

// Current is the line the cursor is on, and false when no line is held.
func (t *Tail) Current() (string, bool) {
	if len(t.lines) == 0 {
		return "", false
	}

	return t.lines[len(t.lines)-1-t.cursor], true
}

// wrap is one held line broken into the rows it takes at width.
func (t *Tail) wrap(i, width int) []string {
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
