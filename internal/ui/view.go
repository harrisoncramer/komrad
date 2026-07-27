package ui

import "claude-agents/internal/errui"

// View renders the active screen. The two takeover screens come first, because
// every screen below reads or writes through the service and none can show
// anything true while something upstream of them is broken. A config error
// outranks an unreachable service: the config is read once at startup, so a panel
// that never parsed it never got as far as talking to the service.
//
// A refused action is different in kind and so renders differently. It banners
// over the screen it happened on rather than replacing it, since the thing the
// error is about is a row in the list underneath.
func (m model) View() string {
	if m.configErr != nil {
		return errui.Config(m.configErr)
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
	case screenForm, screenRename, screenWorkflowForm, screenWorkflowDelete:
		return m.form.View()
	case screenSearch:
		return m.searchView()
	default:
		return m.agentsView()
	}
}
