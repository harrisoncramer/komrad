package ui

import "claude-agents/internal/errui"

// View renders the active screen. The takeover screens come first, because every
// screen below reads or writes through the service and none can show anything true
// while something upstream of them is broken. They are ordered by how early the
// failure happened, so what is on screen is the first thing that went wrong rather
// than the last thing it caused: a config that would not parse outranks a server
// that would not start, which outranks a service that stopped answering.
//
// A refused action is different in kind and so renders differently. It banners
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

	var banner string
	if m.actionErr != nil {
		banner = m.actionErr.Render()
	}

	return banner + m.screenView()
}

// screenView renders whichever screen the panel is on, without the chrome above it.
func (m model) screenView() string {
	switch m.screen {
	case screenForm, screenRename, screenWorkflowForm, screenWorkflowDelete,
		screenScheduleForm, screenScheduleDelete:
		return m.form.View()
	case screenSearch:
		return m.searchView()
	case screenSchedules:
		return m.schedulesView()
	default:
		return m.agentsView()
	}
}
