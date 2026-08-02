package ui

import (
	"strings"

	"weave/internal/errui"
)

// View renders the active screen. The takeover screens come first, since nothing below
// them can show anything true while something upstream is broken, ordered so what is on
// screen is the first thing that went wrong rather than the last thing it caused: a config
// that would not parse outranks a server that would not start, which outranks a service
// that stopped answering.
//
// A refused write banners over the screen it happened on rather than replacing it, since
// what the error is about is a row in the list underneath.
func (m model) View() string {
	if m.configErr != nil {
		return errui.Config(m.width, m.configErr)
	}

	if m.startupErr != nil {
		return errui.Startup(m.width, m.startupErr, m.startupFindings)
	}

	if m.unreachable != nil {
		return errui.Unreachable(m.width, m.addr, m.unreachable)
	}

	// A form waiting on its write is frozen, so it says so. It takes the banner's place,
	// which reads as the refusal being retried.
	var chrome string
	switch {
	case m.pendingForm:
		chrome = dimStyle.Render("  submitting…") + "\n\n"
	case m.banner != nil:
		chrome = m.banner.Render(m.width)
	}

	// Every block a screen builds ends in a newline, and the renderer draws what
	// follows the last one as a line of its own. On a screen built to fill the pane
	// that line is one too many, and the pane scrolls to make room, taking the title
	// off the top.
	return strings.TrimRight(chrome+m.screenView(), "\n")
}

// screenView renders whichever screen the panel is on, without the chrome above it.
func (m model) screenView() string {
	if m.screen.isForm() {
		return m.form.View()
	}

	switch m.screen {
	case screenSearch:
		return m.searchView()
	case screenSchedules:
		return m.schedulesView()
	case screenArchive:
		return m.archiveView()
	case screenActions:
		return m.actionsView()
	default:
		return m.agentsView()
	}
}
