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

// Render lays the screen out, separating sections only when they have content. Everything
// is folded to the pane, prose included, since the renderer above the panel cuts a line
// that overruns rather than wrapping it, and what's lost at the edge is the tail of the
// line, which on a hint is the command it was naming.
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
