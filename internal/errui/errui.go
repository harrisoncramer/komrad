// Package errui draws the failures a person has to see: the full screens weave falls back to
// when it cannot start, and the banner the panel shows when a call it made was refused.
//
// A failure at startup has no panel to report itself inside, since the panel is the thing that
// did not come up, so it is rendered as a screen of its own carrying the reason, the details
// needed to fix it, and the trace id to search the service log by. The banner is the same
// material in one line, for the failures that happen while the panel is already running.
package errui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	Red = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	Dim = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type Screen struct {
	Title string

	Body []string

	Details []Detail

	Hints []string

	Width int
}

type Detail struct {
	Label string
	Value string
}

// Render lays the screen out folded to the pane, since the renderer above the panel cuts a
// line that overruns and what is lost is the command a hint was naming.
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

const maxWidth = 120

const labelWidth = 9

const minWidth = 20

func foldWidth(pane int) int {
	if pane <= 0 {
		return maxWidth
	}

	return min(pane, maxWidth)
}

func styled(style lipgloss.Style, text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = style.Render(line)
	}

	return strings.Join(lines, "\n")
}

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
