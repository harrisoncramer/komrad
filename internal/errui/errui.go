// Package errui renders the panel's errors. Every failure is built here rather than
// formatted wherever it was noticed, so they all read the same: a red headline, prose
// saying what it means for what is on screen, the machine detail, and dim closing hints.
//
// It holds the presentation and nothing about the panel's state, so what a screen says
// cannot depend on which part of the panel built it.
package errui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Both colours are indexes into the sixteen a terminal's own scheme maps rather than hex
// values, so an error screen is drawn in the colours the terminal is already running.
var (
	// Red is the headline colour, the same red the agent list uses for a row that needs
	// attention.
	Red = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	// Dim is the colour of anything secondary: labels, hints, and the panel's own chrome,
	// which shares it so the two cannot drift apart.
	Dim = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// Screen is a failure that takes the whole panel, for when nothing on screen can be
// trusted.
type Screen struct {
	// Title is the headline, the one line saying what is wrong.
	Title string
	// Body explains what the failure means for the panel, one entry per paragraph. Each
	// is folded to the width, so a paragraph is written as one string rather than broken
	// by hand into lines that fit only one pane.
	Body []string
	// Details are the labelled facts worth reading exactly, such as an address or the
	// underlying error. Long values wrap under their label.
	Details []Detail
	// Hints close the screen with what to do about it.
	Hints []string
	// Width is the pane the screen renders into, zero before the first resize.
	Width int
}

// Detail is one labelled fact on a screen.
type Detail struct {
	Label string
	Value string
}

// Render lays the screen out, separating sections only when they have content.
//
// Everything is folded to the pane, prose included, because the renderer above the panel
// cuts a line that overruns rather than wrapping it, and what is lost at the pane edge is
// the tail of the line, which on a hint is the command it was naming.
func (s Screen) Render() string {
	width := foldWidth(s.Width)

	var b strings.Builder

	b.WriteString(styled(Red, fold(s.Title, width, "")) + "\n\n")

	for _, paragraph := range s.Body {
		b.WriteString(fold(paragraph, width, "") + "\n")
	}
	if len(s.Body) > 0 {
		b.WriteString("\n")
	}

	for _, detail := range s.Details {
		// A label wider than the column pushes its value right, so the fold is given
		// the label actually written rather than the column it was meant to fit, or
		// the first line of the value overruns the pane by the overflow.
		label := fmt.Sprintf("%-*s", labelWidth, detail.Label)
		b.WriteString(Dim.Render(label) + fold(detail.Value, width, strings.Repeat(" ", len(label))) + "\n")
	}

	if len(s.Hints) > 0 {
		b.WriteString("\n")
		for _, hint := range s.Hints {
			b.WriteString(styled(Dim, fold(hint, width, "")) + "\n")
		}
	}

	return b.String()
}

// maxWidth is how wide a screen runs in a pane wide enough to allow it, since prose
// stretched across a wide pane reads worse than the same prose in a column.
const maxWidth = 120

// labelWidth is the column values line up in.
const labelWidth = 9

// minWidth is the narrowest anything folds to. A pane too narrow for the label column and
// a readable value beside it would otherwise fold to a column a word wide, which reads
// worse than a value that overruns.
const minWidth = 20

// foldWidth is how wide a screen folds to in a pane of the given width. A pane of unknown
// size folds at the maximum, since that is the only width worth guessing.
func foldWidth(pane int) int {
	if pane <= 0 {
		return maxWidth
	}

	return min(pane, maxWidth)
}

// styled colours text a line at a time, since handing a folded block to a style pads every
// line out to the longest of them.
func styled(style lipgloss.Style, text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}

	return strings.Join(lines, "\n")
}

// fold folds text onto as many lines as it needs to sit inside width, prefixing
// continuations with indent so they read as part of what they continue. The indent comes
// out of the width, so a folded value stays inside the pane under its label.
//
// It wraps rather than truncating, since the useful part of a dial error is at the end,
// and it breaks a word too long to fit at all, which a config path or an address is.
func fold(text string, width int, indent string) string {
	body := max(width-len(indent), minWidth)

	lines := strings.Split(lipgloss.NewStyle().Width(body).Render(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
		if i > 0 {
			lines[i] = indent + lines[i]
		}
	}

	return strings.Join(lines, "\n")
}
