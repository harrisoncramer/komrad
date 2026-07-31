// Package errui renders the panel's errors.
//
// Every failure the panel reports is built here rather than formatted wherever it
// was noticed, so they all read the same: a red headline saying what is wrong,
// plain prose saying what it means for what is on screen, the machine detail under
// it, and dim closing lines saying what to do.
//
// It holds the presentation and nothing about the panel's state. The screens are
// functions of the error and, at most, the address it concerns, so what a screen
// says cannot depend on which part of the panel happened to build it.
package errui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Both colours are indexes into the sixteen a terminal's own scheme maps rather than hex
// values, so an error screen is drawn in the colours the terminal is already running, the
// way the rest of the panel is.
var (
	// Red is the headline colour, the same red the agent list uses for a row that
	// needs attention.
	Red = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	// Dim is the colour of anything secondary: labels, hints, and the panel's own
	// chrome, which shares it so the two cannot drift apart.
	Dim = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// Screen is a failure that takes the whole panel. It is used when nothing on
// screen can be trusted, so there is no point leaving any of it visible.
type Screen struct {
	// Title is the headline, the one line saying what is wrong.
	Title string
	// Body explains what the failure means for the panel, one entry per paragraph.
	// Each is folded to the width, so a paragraph is written as one string rather
	// than broken by hand into lines that would only fit one pane.
	Body []string
	// Details are the labelled facts worth reading exactly, such as an address or
	// the underlying error. Long values wrap under their label.
	Details []Detail
	// Hints close the screen with what to do about it.
	Hints []string
	// Width is the pane the screen is rendering into, in columns. Zero means the
	// panel has not been told its size yet.
	Width int
}

// Detail is one labelled fact on a screen.
type Detail struct {
	Label string
	Value string
}

// Render lays the screen out. Sections are separated only when they have content,
// so a screen with no details does not leave a gap where they would be.
//
// Everything on it is folded to the pane, including the prose, because the panel
// runs in the narrow half of a split and the renderer above it cuts a line that
// overruns the pane rather than wrapping it. An unfolded line loses its tail at the
// pane edge, which on a hint is the command it was naming.
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
		label := fmt.Sprintf("%-*s", labelWidth, detail.Label)
		b.WriteString(Dim.Render(label) + fold(detail.Value, width, indent) + "\n")
	}

	if len(s.Hints) > 0 {
		b.WriteString("\n")
		for _, hint := range s.Hints {
			b.WriteString(styled(Dim, fold(hint, width, "")) + "\n")
		}
	}

	return b.String()
}

// maxWidth is how wide a screen runs in a pane wide enough to allow it. Prose
// stretched the whole way across a wide pane reads worse than the same prose in a
// column, so the pane is a ceiling on the fold rather than the fold itself.
const maxWidth = 120

// labelWidth is the column values line up in.
const labelWidth = 9

// indent lines a wrapped value up under the first line of that value. It is built
// from the label width so the two cannot drift apart.
var indent = strings.Repeat(" ", labelWidth)

// minWidth is the narrowest anything folds to. A pane too narrow to hold the label
// column and a readable value beside it would otherwise fold to a column a word
// wide, and a value that overruns a pane that narrow reads better than one broken
// every other word.
const minWidth = 20

// foldWidth is how wide a screen folds to in a pane of the given width. A pane the
// panel has not been told the size of yet folds at the maximum, since that is the
// only width there is any reason to guess.
func foldWidth(pane int) int {
	if pane <= 0 {
		return maxWidth
	}

	return min(pane, maxWidth)
}

// styled colours text a line at a time. Handing a folded block to a style instead
// pads every line out to the longest of them, which is trailing whitespace on all but
// one line of anything folded.
func styled(style lipgloss.Style, text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}

	return strings.Join(lines, "\n")
}

// fold folds text onto as many lines as it needs to sit inside width, prefixing the
// continuations with indent so they read as part of what they continue rather than as
// a new field. The indent comes out of the width, so a folded value stays inside the
// pane once it is written out under its label.
//
// It wraps rather than truncating, since the useful part of a dial error is at the
// end, and it breaks a word too long to fit at all: a config path or an address is a
// single word that would otherwise overrun on its own.
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
