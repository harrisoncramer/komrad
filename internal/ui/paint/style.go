package paint

import (
	"image/color"
	"strings"
	"unicode/utf8"

	"komrad/internal/glyph"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ansiReset ends every styled run lipgloss writes.
const ansiReset = "\x1b[m"

// resets are the two spellings of a reset a styled line can carry.
var resets = []string{"\x1b[0m", ansiReset}

// Header is the style a list's title is rendered in.
func (p Palette) Header() lipgloss.Style {
	return p.Style(TonePrimary).Bold(true)
}

// Cursor is the style the row cursor is rendered in.
func (p Palette) Cursor() lipgloss.Style {
	return p.Style(TonePrimary).Bold(true)
}

// Title renders a list's title.
func (p Palette) Title(text string) string {
	return p.Header().Render(text)
}

// Match marks a search hit in a list row.
func (p Palette) Match() lipgloss.Style {
	return p.Style(ToneHighlight).Bold(true)
}

// Highlight renders text in a base style with the spans a search picked out rendered in the match
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

// HighlightStyled renders a styled row with the spans a search picked out of its plain text in the
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

// Broken is what a row renders in when what it stands for is gone.
func (p Palette) Broken() lipgloss.Style {
	return p.Style(ToneDanger)
}

// stageMatch marks a match in rendered markdown with a block behind the word.
func (p Palette) stageMatch() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(p.Color(ToneInverse)).Background(p.Color(ToneHighlight)).Bold(true)
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
	switch {
	case width <= 0:
		return ""
	case len(runes) <= width:
		return text
	}

	return string(runes[:width-1]) + "…"
}

// WrapRows is prose wrapped into the column it is rendered in, breaking a token longer than the
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

// PaintRow lays the selection background under one already-coloured line, setting it again after
// every reset that line carries and filling the line out to the pane so it reads as a bar.
func (p Palette) PaintRow(row string, width int) string {
	// The diff viewer threads a background into every fragment it renders instead.
	set := BackgroundSequence(p.Background(ShadeSelection))

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

// Mode is the style a label saying which mode holds the panel is rendered in, in that mode's tone.
func (p Palette) Mode(tone Tone) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(p.Color(tone)).Bold(true)
}

// TmuxBorder is the style tmux renders a pane border in to match the divider.
func (p Palette) TmuxBorder() string {
	return "fg=" + tmuxColor(p.colors.Muted)
}

// TmuxSelection is the style tmux renders a selection in, a bar in the visual mode's tone.
func (p Palette) TmuxSelection() string {
	return "fg=" + tmuxColor(p.colors.Inverse) + ",bg=" + tmuxColor(p.colors.Mode.Visual)
}

// clipStart cuts text to a width from its start, beginning it in an ellipsis.
func clipStart(text string, width int) string {
	runes := []rune(text)
	switch {
	case width <= 0:
		return ""
	case len(runes) <= width:
		return text
	}

	return "…" + string(runes[len(runes)-width+1:])
}

// ClipAround cuts the text either side of a cursor to a width between them, keeping the cursor
// in view: the start goes while the cursor is near the end, and the end goes otherwise.
func ClipAround(before, after string, width int) (string, string) {
	head, tail := len([]rune(before)), len([]rune(after))
	switch {
	case width <= 0:
		return "", ""
	case head+tail <= width:
		return before, after
	case tail < width:
		return clipStart(before, width-tail), after
	}

	kept := min(head, max(width/2, 1))

	return clipStart(before, kept), Clip(after, width-kept)
}

// RenderTyped is text being typed with its cursor in it: a bar after the text while the cursor is at
// its end, and otherwise the character under the cursor rendered in reverse, so the cursor takes no
// column of its own.
func (p Palette) RenderTyped(before, after string, style lipgloss.Style) string {
	if after == "" {
		return style.Render(before) + p.Cursor().Render(glyph.TextCursor)
	}

	under, size := utf8.DecodeRuneInString(after)

	return style.Render(before) + p.Cursor().Reverse(true).Render(string(under)) + style.Render(after[size:])
}

// titledRuleFloor is the narrowest width a rule carries its labels at.
const titledRuleFloor = 24

// titledRuleLead is how many rule glyphs come before a rule's title.
const titledRuleLead = 3

// TitledRuleFits reports whether a rule of width has room for title and tail.
func TitledRuleFits(title, tail string, width int) bool {
	return width >= titledRuleFloor && titledRuleRoom(title, tail) < width
}

func titledRuleRoom(title, tail string) int {
	room := 0
	if w := lipgloss.Width(title); w > 0 {
		room += titledRuleLead + w + 2
	}
	if w := lipgloss.Width(tail); w > 0 {
		room += w + 3
	}

	return room
}

// TitledRule is a muted rule across width columns with title set in near its left end and tail
// against its right.
func (p Palette) TitledRule(title, tail string, width int) string {
	muted := p.Style(ToneMuted)
	if titledRuleRoom(title, tail) == 0 || !TitledRuleFits(title, tail, width) {
		return muted.Render(strings.Repeat(glyph.Rule, max(width, 0)))
	}

	left, right := "", ""
	if lipgloss.Width(title) > 0 {
		left = muted.Render(strings.Repeat(glyph.Rule, titledRuleLead)+" ") + title + muted.Render(" ")
	}
	if lipgloss.Width(tail) > 0 {
		right = muted.Render(" ") + tail + muted.Render(" "+glyph.Rule)
	}

	return left + muted.Render(strings.Repeat(glyph.Rule, width-titledRuleRoom(title, tail))) + right
}
