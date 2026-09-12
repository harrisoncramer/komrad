package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// agentHistoryLimit is how many agents back the history keys reach.
const agentHistoryLimit = 20

// historyWalk is the direction one of the history keys walks in.
type historyWalk int

const (
	walkBack historyWalk = iota
	walkForward
)

// goBackAgent puts the cursor on the agent opened before the one it is on.
func (m *model) goBackAgent() (tea.Model, tea.Cmd) {
	return m.walkHistory(walkBack)
}

// goForwardAgent undoes a step back.
func (m *model) goForwardAgent() (tea.Model, tea.Cmd) {
	return m.walkHistory(walkForward)
}

// walkHistory steps until it reaches an agent still on the list, forgetting the ones that are
// not.
func (m *model) walkHistory(walk historyWalk) (tea.Model, tea.Cmd) {
	for {
		id, ok := m.stepHistory(walk)
		if !ok {
			return *m, nil
		}

		next, cmd, landed := m.jumpToRow(agentRowKey(id))
		if landed {
			return next, cmd
		}

		m.forgetAgentWalked(walk, id)
	}
}

// stepHistory moves the history cursor one agent along the walk.
func (m *model) stepHistory(walk historyWalk) (string, bool) {
	if walk == walkBack {
		return m.history.GoBack()
	}

	return m.history.GoForward()
}

// forgetAgentWalked drops an agent the list no longer holds, leaving the cursor where the next
// step of this walk expects it.
func (m *model) forgetAgentWalked(walk historyWalk, id string) {
	m.history.Remove(id)

	if walk == walkForward {
		m.history.GoBack()
	}
}
