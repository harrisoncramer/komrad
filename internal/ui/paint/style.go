package paint

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ansiReset ends every styled run lipgloss writes.
const ansiReset = "\x1b[0m"

// Header is the style a list's title is drawn in.
func (p Palette) Header() lipgloss.Style {
	return p.Style(ToneAccent).Bold(true)
}

// Cursor is the style the row cursor is drawn in.
func (p Palette) Cursor() lipgloss.Style {
	return p.Style(ToneAccent).Bold(true)
}

// Title draws a list's title.
func (p Palette) Title(text string) string {
	return p.Header().Render(text)
}

// Match marks a search hit in a list row.
func (p Palette) Match() lipgloss.Style {
	return p.Style(ToneWarning).Bold(true)
}

// Broken is what a row draws in when what it stands for is gone.
func (p Palette) Broken() lipgloss.Style {
	return p.Style(ToneDanger)
}

// stageMatch marks a match in rendered markdown, where the warning colour the listing marks with
// is already spoken for by inline code, so the word gets a block behind it instead.
func (p Palette) stageMatch() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(p.danger).Bold(true)
}

// Mark is the shape every optional mark on a row takes: nothing when the fact it stands for is
// not true, otherwise a space and the mark in its own tone.
func (p Palette) Mark(show bool, icon string, tone Tone) string {
	if !show {
		return ""
	}

	return " " + p.Style(tone).Render(icon)
}

// MarkGutter is the column a mark is drawn in, left of every row.
func (p Palette) MarkGutter(mark string) string {
	if mark == "" {
		return "  "
	}

	return p.Style(ToneMark).Render(mark) + " "
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

// Explainer: the fill is measured across the whole string PaintRow is handed, so it holds only
// for a row of a single line. The bar is one colour rather than one per palette: a confirm form
// takes the keys off the list it stands beside, and what has been picked has to stay legible on
// the screen that asks whether to act on it.

// SelectionBackground is the bar a row taken into a selection is drawn under, dark enough that
// the colours the row is already carrying still read through it.
var SelectionBackground = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1e3a5f"}

// PaintRow lays the selection background under one already-coloured line, setting it again after
// every reset that line carries and filling the line out to the pane so it reads as a bar.
func PaintRow(row string, width int) string {
	// The diff viewer threads a background into every fragment it draws instead.
	set := BackgroundSequence(SelectionBackground)
	if set == "" {
		return row
	}

	painted := set + strings.ReplaceAll(row, ansiReset, ansiReset+set)
	if padding := width - lipgloss.Width(row); padding > 0 {
		painted += strings.Repeat(" ", padding)
	}

	return painted + ansiReset
}

// BackgroundSequence is the escape that sets a background and nothing else, which lipgloss will
// only write around text of its own, and is empty under a profile carrying no colour.
func BackgroundSequence(background lipgloss.AdaptiveColor) string {
	hex := background.Light
	if lipgloss.HasDarkBackground() {
		hex = background.Dark
	}

	sequence := lipgloss.ColorProfile().Color(hex).Sequence(true)
	if sequence == "" {
		return ""
	}

	return "\x1b[" + sequence + "m"
}
