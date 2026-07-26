package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// errorStyle colours the header of the unreachable screen, using the same red
// the list uses for a chat that needs attention.
var errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E82424")).Bold(true)

// unreachableView is shown in place of every other screen while the service is
// not answering.
//
// It takes over rather than sitting in a corner because the panel is a view onto
// the service: with the service gone the chat list is a stale snapshot, the
// counts are wrong, and every key that acts on a chat fails. Leaving the list on
// screen made a dead service look like an empty store, which is what an unlucky
// press of the delete key looked like.
//
// There is nothing to press. The list keeps polling underneath, so the panel
// returns to itself within a poll of the service coming back.
func (m model) unreachableView() string {
	var b strings.Builder

	b.WriteString(errorStyle.Render("Cannot reach the server") + "\n\n")
	b.WriteString("The panel could not list your chats, so what it was showing is\n")
	b.WriteString("out of date. Nothing has been deleted.\n\n")

	if m.addr != "" {
		b.WriteString(dimStyle.Render("address  ") + m.addr + "\n")
	}
	if m.unreachable != nil {
		b.WriteString(dimStyle.Render("error    ") + truncateError(m.unreachable.Error()) + "\n")
	}

	b.WriteString("\n" + dimStyle.Render("Start it with: claude-agents serve") + "\n")
	b.WriteString(dimStyle.Render("This screen clears itself once the server answers.") + "\n")

	return b.String()
}

// errorWidth bounds the reported error. A failed dial carries the whole address
// and syscall chain, which wraps into a wall of text that buries the line above
// it explaining what to do.
const errorWidth = 120

// truncateError shortens a long error to one readable line.
func truncateError(msg string) string {
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) <= errorWidth {
		return msg
	}

	return msg[:errorWidth] + "…"
}
