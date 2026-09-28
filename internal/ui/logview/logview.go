// Package logview holds log lines and draws the newest of them.
package logview

import (
	"strings"

	"komrad/lib/xslice"

	"github.com/charmbracelet/x/ansi"
)

// LogViewer holds log lines and draws the newest of them, or older ones once scrolled back.
type LogViewer interface {
	// Append adds lines after the last, dropping the oldest past the ceiling.
	Append(lines []string)
	// Reset drops every line.
	Reset()
	// Len is how many lines are held.
	Len() int
	// Scroll moves the view a number of lines toward the newest for a positive count and toward
	// the oldest for a negative one, stopping at either end.
	Scroll(lines int)
	// Rows is the lines up to the one the view ends on wrapped to width, as many rows of them as
	// fit in height, holding the view where it first fills height once scrolled past the oldest.
	Rows(width, height int) []string
}

type tail struct {
	lines   []string
	ceiling int
	// back is how many lines the view ends before the newest, zero while it follows the log.
	back int
}

// NewTail is a LogViewer that keeps at most ceiling lines.
func NewTail(ceiling int) LogViewer {
	return &tail{ceiling: ceiling}
}

func (t *tail) Append(lines []string) {
	t.lines = append(t.lines, xslice.Map(lines, sanitize)...)
	if t.back > 0 {
		t.back += len(lines)
	}

	if over := len(t.lines) - t.ceiling; over > 0 {
		t.lines = append([]string(nil), t.lines[over:]...)
	}
	t.Scroll(0)
}

func (t *tail) Reset() {
	t.lines, t.back = nil, 0
}

func (t *tail) Len() int {
	return len(t.lines)
}

func (t *tail) Scroll(lines int) {
	t.back = min(max(t.back-lines, 0), max(len(t.lines)-1, 0))
}

func (t *tail) Rows(width, height int) []string {
	if height <= 0 {
		return nil
	}

	t.back = min(t.back, t.furthestBack(width, height))

	var rows []string
	for i := len(t.lines) - 1 - t.back; i >= 0 && len(rows) < height; i-- {
		rows = append(t.wrap(i, width), rows...)
	}

	return rows[max(len(rows)-height, 0):]
}

// furthestBack is the most lines the view can end before the newest and still fill height rows.
func (t *tail) furthestBack(width, height int) int {
	filled := 0
	for i := range t.lines {
		filled += len(t.wrap(i, width))
		if filled >= height {
			return len(t.lines) - 1 - i
		}
	}

	return 0
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
