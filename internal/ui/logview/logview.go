// Package logview holds log lines and draws the newest of them.
package logview

import (
	"strings"

	"komrad/lib/xslice"

	"github.com/charmbracelet/x/ansi"
)

// LogViewer holds log lines and draws the newest of them.
type LogViewer interface {
	// Append adds lines after the last, dropping the oldest past the ceiling.
	Append(lines []string)
	// Reset drops every line.
	Reset()
	// Len is how many lines are held.
	Len() int
	// Rows is the newest lines wrapped to width, as many rows of them as fit in height.
	Rows(width, height int) []string
}

type tail struct {
	lines   []string
	ceiling int
}

// NewTail is a LogViewer that keeps at most ceiling lines.
func NewTail(ceiling int) LogViewer {
	return &tail{ceiling: ceiling}
}

func (t *tail) Append(lines []string) {
	t.lines = append(t.lines, xslice.Map(lines, sanitize)...)

	if over := len(t.lines) - t.ceiling; over > 0 {
		t.lines = append([]string(nil), t.lines[over:]...)
	}
}

func (t *tail) Reset() {
	t.lines = nil
}

func (t *tail) Len() int {
	return len(t.lines)
}

func (t *tail) Rows(width, height int) []string {
	if height <= 0 {
		return nil
	}

	var rows []string
	for i := len(t.lines) - 1; i >= 0 && len(rows) < height; i-- {
		rows = append(strings.Split(ansi.Hardwrap(t.lines[i], max(width, 1), true), "\n"), rows...)
	}

	return rows[max(len(rows)-height, 0):]
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
