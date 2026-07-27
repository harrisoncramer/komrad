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

var (
	// Red is the headline colour, the same red the agent list uses for a row that
	// needs attention.
	Red = lipgloss.NewStyle().Foreground(lipgloss.Color("#E82424")).Bold(true)
	// Dim is the colour of anything secondary: labels, hints, and the panel's own
	// chrome, which shares it so the two cannot drift apart.
	Dim = lipgloss.NewStyle().Foreground(lipgloss.Color("#727169"))
)

// Screen is a failure that takes the whole panel. It is used when nothing on
// screen can be trusted, so there is no point leaving any of it visible.
type Screen struct {
	// Title is the headline, the one line saying what is wrong.
	Title string
	// Body explains what the failure means for the panel, one entry per line.
	Body []string
	// Details are the labelled facts worth reading exactly, such as an address or
	// the underlying error. Long values wrap under their label.
	Details []Detail
	// Hints close the screen with what to do about it.
	Hints []string
}

// Detail is one labelled fact on a screen.
type Detail struct {
	Label string
	Value string
}

// Render lays the screen out. Sections are separated only when they have content,
// so a screen with no details does not leave a gap where they would be.
func (s Screen) Render() string {
	var b strings.Builder

	b.WriteString(Red.Render(s.Title) + "\n\n")

	for _, line := range s.Body {
		b.WriteString(line + "\n")
	}
	if len(s.Body) > 0 {
		b.WriteString("\n")
	}

	for _, detail := range s.Details {
		label := fmt.Sprintf("%-*s", LabelWidth, detail.Label)
		b.WriteString(Dim.Render(label) + Wrap(detail.Value) + "\n")
	}

	if len(s.Hints) > 0 {
		b.WriteString("\n")
		for _, hint := range s.Hints {
			b.WriteString(Dim.Render(hint) + "\n")
		}
	}

	return b.String()
}

// Width is how wide a reported value runs before it wraps. A failed dial carries
// the whole address and syscall chain, and the part saying what went wrong is
// usually at the end, so the value is wrapped whole rather than cut.
const Width = 120

// LabelWidth is the column values line up in.
const LabelWidth = 9

// Indent lines a wrapped value up under the first line of that value. It is built
// from the label width so the two cannot drift apart.
var Indent = strings.Repeat(" ", LabelWidth)

// Wrap folds a long value onto as many lines as it needs, indenting the
// continuations under the first line so they read as part of the same value rather
// than as a new field.
func Wrap(msg string) string {
	lines := strings.Split(valueStyle.Render(msg), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
		if i > 0 {
			lines[i] = Indent + lines[i]
		}
	}

	return strings.Join(lines, "\n")
}

// valueStyle carries only the wrap width. A value is left uncoloured so it reads
// as the detail under the headline.
var valueStyle = lipgloss.NewStyle().Width(Width)
