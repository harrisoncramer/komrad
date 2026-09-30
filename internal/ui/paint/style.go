package paint

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ansiReset ends every styled run lipgloss writes.
const ansiReset = "\x1b[m"

// resets are the two spellings of a reset a styled line can carry.
var resets = []string{"\x1b[0m", ansiReset}

// Header is the style a list's title is drawn in.
func (p Palette) Header() lipgloss.Style {
	return p.Style(ToneAccent).Bold(true)
}

// Cursor is the style the row cursor is drawn in.
func (p Palette) Cursor() lipgloss.Style {
	if p.blinkedOut {
		return p.Style(ToneMuted).Bold(true)
	}

	return p.Style(ToneAccent).Bold(true)
}

// Blinking is the palette with its cursor style on the given beat of the blink.
func (p Palette) Blinking(out bool) Palette {
	p.blinkedOut = out

	return p
}

// Title draws a list's title.
func (p Palette) Title(text string) string {
	return p.Header().Render(text)
}

// Match marks a search hit in a list row.
func (p Palette) Match() lipgloss.Style {
	return p.Style(ToneWarning).Bold(true)
}

// Highlight draws text in a base style with the spans a search picked out drawn in the match
// style, skipping a span that runs backwards or past the end.
func (p Palette) Highlight(text string, spans []Span, base lipgloss.Style) string {
	var b strings.Builder
	at := 0
	for _, span := range spans {
		if span.Start < at || span.End > len(text) {
			continue
		}

		writeStyled(&b, base, text[at:span.Start])
		writeStyled(&b, p.Match(), text[span.Start:span.End])
		at = span.End
	}

	writeStyled(&b, base, text[at:])

	return b.String()
}

// PlainText is a styled row without its escape sequences.
func PlainText(row string) string {
	var b strings.Builder
	walkStyled(row, func(piece string, isText bool) {
		if isText {
			b.WriteString(piece)
		}
	})

	return b.String()
}

// HighlightStyled draws a styled row with the spans a search picked out of its plain text in the
// match style, leaving the row's own styling in force around them.
func (p Palette) HighlightStyled(row string, spans []Span) string {
	if len(spans) == 0 {
		return row
	}

	var b strings.Builder
	at, next, open := 0, 0, false
	walkStyled(row, func(piece string, isText bool) {
		if !isText {
			if open {
				b.WriteString(MarkClose)
				open = false
			}
			b.WriteString(piece)

			return
		}

		for next < len(spans) && spans[next].End <= at {
			next++
		}

		inside := next < len(spans) && spans[next].Start <= at
		switch {
		case inside && !open:
			b.WriteString(MarkOpen)
		case !inside && open:
			b.WriteString(MarkClose)
		}
		open = inside

		b.WriteString(piece)
		at += len(piece)
	})

	if open {
		b.WriteString(MarkClose)
	}

	return styleMarks(b.String(), p.stageMatch())
}

// walkStyled hands each grapheme and each escape sequence of a styled row to visit in order.
func walkStyled(row string, visit func(piece string, isText bool)) {
	var state byte
	for row != "" {
		piece, _, n, next := ansi.DecodeSequence(row, state, nil)
		if n <= 0 {
			piece, n = row[:1], 1
		}

		visit(piece, piece[0] >= ' ' && piece[0] != 0x7f)
		row, state = row[n:], next
	}
}

// writeStyled adds a stretch of text in its style, an empty one carrying no escapes of its own.
func writeStyled(b *strings.Builder, style lipgloss.Style, segment string) {
	if segment == "" {
		return
	}

	b.WriteString(style.Render(segment))
}

// Broken is what a row draws in when what it stands for is gone.
func (p Palette) Broken() lipgloss.Style {
	return p.Style(ToneDanger)
}

// stageMatch marks a match in rendered markdown, where the warning colour the listing marks with
// is already spoken for by inline code, so the word gets a block behind it instead.
func (p Palette) stageMatch() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(Black).Background(p.danger.color()).Bold(true)
}

// Mark is the shape every optional mark on a row takes: nothing when the fact it stands for is
// not true, otherwise a space and the mark in its own tone.
func (p Palette) Mark(show bool, icon string, tone Tone) string {
	if !show {
		return ""
	}

	return " " + p.Style(tone).Render(icon)
}

// Clip cuts text to a width, ending it in an ellipsis.
func Clip(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}

	return string(runes[:width-1]) + "…"
}

// WrapRows is prose wrapped into the column it is drawn in, breaking a token longer than the
// column.
func WrapRows(text string, width int) []string {
	width = max(width, 1)

	rows := []string{}
	for _, row := range strings.Split(ansi.Wrap(text, width, ""), "\n") {
		// Wrap misplaces its own breakpoints, so a row can come back wider than the column it
		// was given, and the overrun is cut onto a row of its own rather than dropped.
		rows = append(rows, strings.Split(ansi.Hardwrap(row, width, true), "\n")...)
	}

	return rows
}

// SelectionBackground is the bar a row taken into a selection is drawn under, dark enough that
// the colours the row is already carrying still read through it.
var SelectionBackground = Adaptive{Light: lipgloss.Color("#dbeafe"), Dark: lipgloss.Color("#1e3a5f")}

// PaintRow lays the selection background under one already-coloured line, setting it again after
// every reset that line carries and filling the line out to the pane so it reads as a bar.
func PaintRow(row string, width int) string {
	// The diff viewer threads a background into every fragment it draws instead.
	set := BackgroundSequence(SelectionBackground)

	painted := set + strings.NewReplacer(resets[0], ansiReset+set, resets[1], ansiReset+set).Replace(row)
	if padding := width - lipgloss.Width(row); padding > 0 {
		painted += strings.Repeat(" ", padding)
	}

	return painted + ansiReset
}

// BackgroundSequence is the escape that sets a background and nothing else, which lipgloss will
// only write around text of its own.
func BackgroundSequence(background color.Color) string {
	return ansi.NewStyle().BackgroundColor(background).String()
}

// Visual is the bar the header says visual mode is on in.
func (p Palette) Visual() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.Color(ToneOther)).Foreground(Black).Bold(true)
}
