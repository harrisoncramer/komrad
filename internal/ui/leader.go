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

func (m *model) holdLeader() {
	if m.session == nil {
		return
	}

	m.session.HoldLeader(m.settings.Pane)
}

// routeLeader answers the leader and the key after it.
func (m *model) routeLeader(key tea.KeyPressMsg) (tea.Cmd, bool) {
	pressed := key.String()

	if !m.leading {
		if pressed != leaderKey {
			return nil, false
		}

		m.leading = true

		return nil, true
	}

	m.leading = false

	switch pressed {
	case copyKey:
		return m.toggleCopy(), true
	case strings.ToLower(paneCopyKey):
		m.term.paneCopying = true

		return nil, true
	case strings.ToLower(paneNormalKey):
		m.term.paneCopying = false

		return nil, true
	case pickerKey:
		return m.togglePicker(), true
	case leaderKey:
		return m.toggleStop(), true
	}

	return nil, true
}

// togglePicker freezes the window under the pane picker, or thaws it when the picker holds it.
func (m *model) togglePicker() tea.Cmd {
	switch {
	case m.picker.freezing, m.term.copying:
		return nil
	case m.picker.frozen != "":
		m.thaw()

		return nil
	}

	return m.freeze()
}
