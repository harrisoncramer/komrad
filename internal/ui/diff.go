package ui

import (
	"slices"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
)

type diffClosedMsg struct {
	err error
}

// openAgentDiff opens the hovered agent's diff in a popup over the panel.
//
// The popup runs on the manager's own tmux server, where the panel's client has none of its
// own up, and tmux gives it the keyboard while it is there. The panel keeps running behind
// it, blind, and hears about it again when the popup closes.
func (m model) openAgentDiff(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	if !m.isReviewable(agent) {
		return m, nil
	}

	tmux, pane := m.tmux, m.pane
	argv := m.diffArgv(agent.ID)

	return m, func() tea.Msg {
		return diffClosedMsg{err: tmux.DisplayPopup(pane, argv...)}
	}
}

func (m model) diffArgv(agentID string) []string {
	self := m.self
	if self == "" {
		self = "weave"
	}

	argv := []string{self, "diff", agentID}
	for _, path := range m.configPaths {
		argv = append(argv, "--config", path)
	}

	return argv
}

// isReviewable reports whether an agent has a diff worth opening, which means a worktree of
// its own that is still on disk. An agent sharing the user's checkout has no base branch to
// be measured against.
func (m model) isReviewable(agent agentstore.Agent) bool {
	if agent.Cwd == "" || agent.WorktreeRemoved {
		return false
	}

	index := slices.IndexFunc(m.listing.workflows, func(w workflowstore.Workflow) bool { return w.ID == agent.WorkflowID })
	if index < 0 {
		return false
	}

	return m.listing.workflows[index].Worktree.Enabled
}
