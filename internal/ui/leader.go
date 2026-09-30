package ui

import (
	tea "charm.land/bubbletea/v2"
)

// leaderKey is the leader as the panel reads it.
const leaderKey = "ctrl+space"

func (m model) holdLeader() {
	if m.session == nil {
		return
	}

	m.session.HoldLeader(m.settings.Pane)
}

// routeLeader answers the leader and the key after it.
func (m model) routeLeader(key tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	pressed := key.String()

	if !m.leading {
		if pressed != leaderKey {
			return m, nil, false
		}

		m.leading = true

		return m, nil, true
	}

	m.leading = false

	switch pressed {
	case "v":
		next, cmd := m.toggleFrozen(frozenVisual)

		return next, cmd, true
	case "j":
		next, cmd := m.toggleFrozen(frozenPicker)

		return next, cmd, true
	}

	return m, nil, true
}

// toggleFrozen freezes the window in a mode, or thaws it when that mode already holds it.
func (m model) toggleFrozen(mode frozenMode) (tea.Model, tea.Cmd) {
	switch {
	case m.visual.freezing:
		return m, nil
	case m.visual.frozen != "" && m.visual.mode == mode:
		return m.thaw(), nil
	case m.visual.frozen != "":
		return m, nil
	}

	return m.freeze(mode)
}
