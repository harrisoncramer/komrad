package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// agentHistoryLimit is how many agents back the history keys reach.
const agentHistoryLimit = 20

// goBackAgent puts the cursor on the agent opened before the one it is on.
func (m *model) goBackAgent() (tea.Model, tea.Cmd) {
	return m.walkHistory(m.history.GoBack)
}

// goForwardAgent undoes a step back.
func (m *model) goForwardAgent() (tea.Model, tea.Cmd) {
	return m.walkHistory(m.history.GoForward)
}

// walkHistory steps until it reaches an agent still on the list, forgetting the ones that are
// not.
func (m *model) walkHistory(step func() (string, bool)) (tea.Model, tea.Cmd) {
	for {
		id, ok := step()
		if !ok {
			return *m, nil
		}

		next, cmd, landed := m.jumpToRow(agentRowKey(id))
		if landed {
			return next, cmd
		}

		m.history.Remove(id)
	}
}
