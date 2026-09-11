package paint

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ansiReset ends every styled run lipgloss writes.
const ansiReset = "\x1b[0m"

// The four styles a list wears, in the pair the focus picks between.
var (
	HeaderStyle        = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	CursorStyle        = lipgloss.NewStyle().Foreground(Accent).Bold(true)
	BlurredHeaderStyle = lipgloss.NewStyle().Foreground(Muted).Bold(true)
	BlurredCursorStyle = lipgloss.NewStyle().Bold(true)
)

// MatchStyle marks a search hit in a list row.
var MatchStyle = lipgloss.NewStyle().Foreground(Warning).Bold(true)

// stageMatchStyle marks a match in rendered markdown, where the warning colour the listing marks
// with is already spoken for by inline code, so the word gets a block behind it instead.
var stageMatchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(Muted)

// BrokenStyle is what a row draws in when what it stands for is gone.
var BrokenStyle = lipgloss.NewStyle().Foreground(Danger)

// SelectionBackground is the bar a row taken into a selection is drawn under, dark enough that
// the colours the row is already carrying still read through it.
var SelectionBackground = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1e3a5f"}

// TitleOf draws a list's title in the style its focus calls for.
func TitleOf(focused bool, text string) string {
	return TitleStyleFor(focused).Render(text)
}

// TitleStyleFor is the title style of a list that holds the keys or does not.
func TitleStyleFor(focused bool) lipgloss.Style {
	if focused {
		return HeaderStyle
	}

	return BlurredHeaderStyle
}

// CursorStyleFor is the row cursor's style on a list that holds the keys or does not.
func CursorStyleFor(focused bool) lipgloss.Style {
	if focused {
		return CursorStyle
	}

	return BlurredCursorStyle
}

// IconMark is the shape every optional mark on a row takes: nothing when the fact it stands for
// is not true, otherwise a space and the mark in its own colour.
func IconMark(show bool, icon string, color lipgloss.Color) string {
	if !show {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(color).Render(icon)
}

// MarkGutter is the column a mark is drawn in, left of every row, so the letters a person types
// to jump to rows read down one edge rather than being hunted for along a row.
func MarkGutter(mark string) string {
	if mark == "" {
		return "  "
	}

	return lipgloss.NewStyle().Foreground(Mark).Render(mark) + " "
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
// column rather than handing back a row the renderer cuts, which drops the tail of a path.
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
// for a row of a single line.

// PaintRow lays a background under one already-coloured line, setting it again after every reset
// that line carries and filling the line out to the pane so it reads as a bar.
func PaintRow(row string, background lipgloss.AdaptiveColor, width int) string {
	// The diff viewer threads a background into every fragment it draws instead.
	set := BackgroundSequence(background)
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
