// Package logview holds log lines and draws them a window at a time.
package logview

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/lib/xslice"

	"github.com/charmbracelet/x/ansi"
)

// LogViewer holds log lines and draws them a window at a time.
type LogViewer interface {
	// Append adds lines after the last, dropping the oldest past the ceiling.
	Append(lines []string)
	// Reset drops every line and follows the newest again.
	Reset()
	// Move moves the cursor by delta lines, and moving up stops following.
	Move(delta int)
	// Top moves the cursor to the oldest line and stops following.
	Top()
	// Bottom moves the cursor to the newest line and follows it.
	Bottom()
	// IsFollowing reports whether the cursor stays on the newest line as lines arrive.
	IsFollowing() bool
	// Len is how many lines are held.
	Len() int
	// Rows is the window of wrapped rows that keeps the cursor in view.
	Rows(width, height int) []string
}

type tail struct {
	lines     []string
	ceiling   int
	cursor    int
	top       int
	following bool
}

// NewTail is a LogViewer that follows the newest line and keeps at most ceiling lines.
func NewTail(ceiling int) LogViewer {
	return &tail{ceiling: ceiling, following: true}
}

func (t *tail) Append(lines []string) {
	t.lines = append(t.lines, xslice.Map(lines, sanitize)...)

	if over := len(t.lines) - t.ceiling; over > 0 {
		t.lines = append([]string(nil), t.lines[over:]...)
		t.cursor = max(t.cursor-over, 0)
		t.top = max(t.top-over, 0)
	}

	if t.following {
		t.cursor = max(len(t.lines)-1, 0)
	}
}

func (t *tail) Reset() {
	t.lines = nil
	t.cursor = 0
	t.top = 0
	t.following = true
}

func (t *tail) Move(delta int) {
	if len(t.lines) == 0 {
		return
	}

	t.cursor = min(max(t.cursor+delta, 0), len(t.lines)-1)
	if delta < 0 {
		t.following = false
	}
}

func (t *tail) Top() {
	t.cursor = 0
	t.following = false
}

func (t *tail) Bottom() {
	t.cursor = max(len(t.lines)-1, 0)
	t.following = true
}

func (t *tail) IsFollowing() bool {
	return t.following
}

func (t *tail) Len() int {
	return len(t.lines)
}

func (t *tail) Rows(width, height int) []string {
	if len(t.lines) == 0 || height <= 0 {
		return nil
	}

	textWidth := max(width-ansi.StringWidth(glyph.Focus), 1)
	wrapped := func(i int) []string {
		return strings.Split(ansi.Hardwrap(t.lines[i], textWidth, true), "\n")
	}

	t.top = min(t.top, t.cursor)
	if t.rowsBetween(t.top, t.cursor, height, wrapped) > height {
		t.top = t.firstFitting(t.cursor, height, wrapped)
	}
	if t.rowsBetween(t.top, len(t.lines)-1, height, wrapped) < height {
		t.top = min(t.top, t.firstFitting(len(t.lines)-1, height, wrapped))
	}

	var rows []string
	for i := t.top; i < len(t.lines) && len(rows) < height; i++ {
		for j, row := range wrapped(i) {
			mark := " "
			if i == t.cursor && j == 0 {
				mark = glyph.Focus
			}
			rows = append(rows, mark+row)
		}
	}

	return rows[:min(len(rows), height)]
}

func (t *tail) rowsBetween(from, to, ceiling int, wrapped func(int) []string) int {
	rows := 0
	for i := from; i <= to && rows <= ceiling; i++ {
		rows += len(wrapped(i))
	}

	return rows
}

func (t *tail) firstFitting(last, height int, wrapped func(int) []string) int {
	rows := len(wrapped(last))
	first := last
	for first > 0 {
		rows += len(wrapped(first - 1))
		if rows > height {
			break
		}
		first--
	}

	return first
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
