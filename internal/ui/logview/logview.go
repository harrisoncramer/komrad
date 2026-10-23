package logview

import (
	"io"
	"strings"

	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Tail holds log lines and renders the newest of them, or the ones around a cursor moved back.
type Tail struct {
	lines   []string
	ceiling int
	format  Format
	// cursor is how many lines the cursor stands before the newest, zero while it follows the log.
	cursor int
	// back is how many lines the view ends before the newest.
	back int
	// scrolled says the view moved last.
	scrolled bool
}

// Format is how a Tail renders each line it holds.
type Format int

const (
	// FormatRaw renders each line as it was written.
	FormatRaw Format = iota
	// FormatCompact renders each structured server log line on one row.
	FormatCompact
)

// NewTail is a Tail that keeps at most ceiling lines.
func NewTail(ceiling int) *Tail {
	return &Tail{ceiling: ceiling}
}

// Append adds lines after the last, dropping the oldest past the ceiling.
func (t *Tail) Append(lines []string) {
	t.lines = append(t.lines, lines...)
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

// SetFormat renders the lines held in format from the next render on.
func (t *Tail) SetFormat(format Format) {
	t.format = format
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

// MoveTo puts the cursor on a line counted from the oldest, leaving the view to follow it.
func (t *Tail) MoveTo(line int) {
	t.cursor = cursor.Clamp(len(t.lines)-1-line, len(t.lines))
	t.scrolled = false
}

// Fit moves the cursor and the view so that both fit in height rows of lines wrapped to width.
func (t *Tail) Fit(width, height int) {
	newest := len(t.lines) - 1
	if span, ok := t.span(t.wrapper(width), height); ok {
		t.cursor, t.back = newest-span.Cursor, newest-span.Last
	}
}

// Rows is the lines wrapped to width, as many rows of them as fit in height with the cursor's
// line among them, and the row the cursor's line starts on.
func (t *Tail) Rows(width, height int) ([]string, int) {
	wrap := t.wrapper(width)
	span, ok := t.span(wrap, height)
	if !ok {
		return nil, -1
	}

	var rows []string
	for i := span.First; i <= span.Last; i++ {
		rows = append(rows, wrap(i)...)
	}

	return rows[span.Cut:], span.CursorRow
}

// Lines is the line each of the rows Rows gives is wrapped from.
func (t *Tail) Lines(width, height int) []int {
	wrap := t.wrapper(width)
	span, ok := t.span(wrap, height)
	if !ok {
		return nil
	}

	var lines []int
	for i := span.First; i <= span.Last; i++ {
		for range wrap(i) {
			lines = append(lines, i)
		}
	}

	return lines[span.Cut:]
}

// span is the lines that fit in height rows around the cursor, or around the view once it scrolled.
func (t *Tail) span(wrap func(int) []string, height int) (cursor.Span, bool) {
	window := cursor.Window{Height: height, Rows: func(i int) int { return len(wrap(i)) }}
	keep := window.KeepCursor
	if t.scrolled {
		keep = window.KeepView
	}

	newest := len(t.lines) - 1

	return keep(len(t.lines), newest-t.cursor, newest-t.back)
}

// wrapper breaks held lines into the rows they take at width, wrapping each line once.
func (t *Tail) wrapper(width int) func(int) []string {
	wrapped := make(map[int][]string)

	return func(i int) []string {
		if _, ok := wrapped[i]; !ok {
			wrapped[i] = t.wrap(i, width)
		}

		return wrapped[i]
	}
}

// Current is the line the cursor is on, and false when no line is held.
func (t *Tail) Current() (string, bool) {
	if len(t.lines) == 0 {
		return "", false
	}

	return sanitize(t.lines[len(t.lines)-1-t.cursor]), true
}

// wrap is one held line broken into the rows it takes at width.
func (t *Tail) wrap(i, width int) []string {
	return strings.Split(hardwrap(t.render(t.lines[i]), max(width, 1)), "\n")
}

// hardwrap is text broken at width with every row opening and closing its own style.
func hardwrap(text string, width int) string {
	wrapped := ansi.Hardwrap(text, width, true)
	var rows strings.Builder
	writer := lipgloss.NewWrapWriter(&rows)
	if _, err := io.WriteString(writer, wrapped); err != nil {
		return wrapped
	}
	if err := writer.Close(); err != nil {
		return wrapped
	}

	return rows.String()
}

// render is one held line as its format renders it.
func (t *Tail) render(line string) string {
	if t.format == FormatCompact {
		return Compact(paint.Panel, line)
	}

	return sanitize(line)
}

// sanitize strips escape sequences and control characters from a line the panel renders itself.
func sanitize(line string) string {
	return strings.ReplaceAll(paint.StripControls(line), "\t", " ")
}
