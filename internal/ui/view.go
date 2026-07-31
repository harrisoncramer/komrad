package ui

import (
	"strings"

	"weave/internal/errui"
)

// View renders the active screen. The takeover screens come first, because every
// screen below reads or writes through the service and none can show anything true
// while something upstream of them is broken. They are ordered by how early the
// failure happened, so what is on screen is the first thing that went wrong rather
// than the last thing it caused: a config that would not parse outranks a server
// that would not start, which outranks a service that stopped answering.
//
// A refused write is different in kind and so renders differently. It banners
// over the screen it happened on rather than replacing it, since the thing the
// error is about is a row in the list underneath.
func (m model) View() string {
	if m.configErr != nil {
		return errui.Config(m.configErr)
	}

	if m.startupErr != nil {
		return errui.Startup(m.startupErr)
	}

	if m.unreachable != nil {
		return errui.Unreachable(m.addr, m.unreachable)
	}

	var chrome string
	if m.banner != nil {
		chrome = m.banner.Render()
	}

	// Every block a screen builds ends in a newline, and the renderer draws the
	// nothing after that last one as a line of its own. On a screen built to fill
	// the pane that line is one too many, and the pane scrolls by one to make room
	// for it, taking the title off the top.
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
	case screenTrash:
		return m.trashView()
	case screenActions:
		return m.actionsView()
	default:
		return m.agentsView()
	}
}
