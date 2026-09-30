package ui

import (
	"komrad/internal/assemble"

	tea "charm.land/bubbletea/v2"
)

const strandedSaid = "I paused you and lost track of the pause, so I have let you go now. " +
	"Look at your worktree as it stands before carrying on. It may have changed while you were paused."

type strandedHoldReactor struct{}

func (strandedHoldReactor) react(m *model, changed assemble.Changed) tea.Cmd {
	if !changed.Replaced {
		return nil
	}

	var cmds []tea.Cmd
	for _, agent := range m.listing.liveAgents() {
		if agent.HeldAt == nil || m.ownsHold(agent.ID) {
			continue
		}

		cmds = append(cmds, m.releaseCmd("Could not let "+agent.Name+" go", agent, strandedSaid))
	}

	return tea.Batch(cmds...)
}

func (m *model) ownsHold(agentID string) bool {
	if edit := m.agents.editing; edit != nil && edit.agent.ID == agentID && (edit.pausing || edit.ourPause) {
		return true
	}

	hold := m.review.hold

	return m.review.agentID == agentID && (hold.pausing || hold.ourPause)
}
