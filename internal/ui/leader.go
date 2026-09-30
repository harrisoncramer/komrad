package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// leaderKey is the leader as the panel reads it.
const leaderKey = "ctrl+space"

// pickerKey is the leader's key that toggles the pane picker.
const pickerKey = "j"

// copyKey is the leader's key that starts copy-mode in the pane holding the keys.
const copyKey = "v"

// paneCopyKey and paneNormalKey are the leader's keys tmux sends the panel when the pane holding
// the keys enters copy-mode or leaves it.
const (
	paneCopyKey   = "F11"
	paneNormalKey = "F12"
)

// leaderPanelKeys are the leader's keys the panel answers itself.
var leaderPanelKeys = []string{pickerKey}

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
	case copyKey:
		next, cmd := m.toggleCopy()

		return next, cmd, true
	case strings.ToLower(paneCopyKey):
		m.paneCopying = true

		return m, nil, true
	case strings.ToLower(paneNormalKey):
		m.paneCopying = false

		return m, nil, true
	case pickerKey:
		next, cmd := m.togglePicker()

		return next, cmd, true
	case leaderKey:
		next, cmd := m.toggleStop()

		return next, cmd, true
	}

	return m, nil, true
}

// togglePicker freezes the window under the pane picker, or thaws it when the picker holds it.
func (m model) togglePicker() (tea.Model, tea.Cmd) {
	switch {
	case m.picker.freezing, m.copying:
		return m, nil
	case m.picker.frozen != "":
		return m.thaw(), nil
	}

	return m.freeze()
}
