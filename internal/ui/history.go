package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// agentHistoryLimit is how many agents back the history keys reach.
const agentHistoryLimit = 20

// goBackAgent puts the cursor on the agent opened before the one it is on.
func (m *model) goBackAgent() (tea.Model, tea.Cmd) {
	return m.walkHistory(walkHistoryParams{step: m.history.GoBack})
}

// goForwardAgent undoes a step back.
func (m *model) goForwardAgent() (tea.Model, tea.Cmd) {
	return m.walkHistory(walkHistoryParams{
		step:        m.history.GoForward,
		afterForget: m.history.GoBack,
	})
}

// walkHistoryParams is one direction of a walk and the step its forgetting needs afterwards.
type walkHistoryParams struct {
	step        func() (string, bool)
	afterForget func() (string, bool)
}

// walkHistory steps until it reaches an agent still on the list, forgetting the ones that are
// not.
func (m *model) walkHistory(params walkHistoryParams) (tea.Model, tea.Cmd) {
	for {
		id, ok := params.step()
		if !ok {
			return *m, nil
		}

		next, cmd, landed := m.jumpToRow(agentRowKey(id))
		if landed {
			return next, cmd
		}

		m.history.Remove(id)

		if params.afterForget != nil {
			params.afterForget()
		}
	}
}
