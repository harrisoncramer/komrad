package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// errorStyle colours the header of the unreachable screen, using the same red
// the list uses for an agent that needs attention.
var errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E82424")).Bold(true)

// unreachableView is shown in place of every other screen while the service is not
// answering.
//
// It takes over rather than sitting in a corner because the panel is a view onto
// the service. With the service gone the agent list is a stale snapshot, the counts
// are wrong, and every key that acts on an agent fails. Left on screen, a dead
// service reads as an empty store.
//
// There is nothing to press. The list keeps polling underneath, so the panel
// returns to itself within a poll of the service coming back.
func (m model) unreachableView() string {
	var b strings.Builder

	b.WriteString(errorStyle.Render("Cannot reach the server") + "\n\n")
	b.WriteString("The panel could not list your agents, so what it was showing is\n")
	b.WriteString("out of date. Nothing has been deleted.\n\n")

	if m.addr != "" {
		b.WriteString(dimStyle.Render("address  ") + m.addr + "\n")
	}
	if m.unreachable != nil {
		b.WriteString(dimStyle.Render("error    ") + wrapError(m.unreachable.Error()) + "\n")
	}

	b.WriteString("\n" + dimStyle.Render("Start it with: claude-agents serve") + "\n")
	b.WriteString(dimStyle.Render("This screen clears itself once the server answers.") + "\n")

	return b.String()
}

// errorWidth is how wide the reported error is allowed to run before it wraps. A
// failed dial carries the whole address and syscall chain, and the part saying what
// went wrong is usually at the end, so the error is wrapped whole rather than
// cut.
const errorWidth = 120

// errorIndent lines wrapped error text up under the first line, so the wrapped
// lines read as part of the error rather than as a new field.
const errorIndent = "         "

// wrapError folds a long error onto as many lines as it needs.
func wrapError(msg string) string {
	lines := strings.Split(errorTextStyle.Render(msg), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
		if i > 0 {
			lines[i] = errorIndent + lines[i]
		}
	}

	return strings.Join(lines, "\n")
}

// errorTextStyle carries only the wrap width. The error is left uncoloured so it
// reads as the detail under the header.
var errorTextStyle = lipgloss.NewStyle().Width(errorWidth)
