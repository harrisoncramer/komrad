package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// jumpMenu is the menu landing on the rows related to an agent, less the screen it was opened from.
func jumpMenu(m *model, from screen, agentID string) submenu {
	agentRow, found := m.listing.anyAgent(agentID)
	target := screenTarget{agentID: agentID}

	var entries []submenuEntry
	if from != screenAgents {
		agent := openEntry("agent", m.keys.JumpMenu.Agent, func(m *model) tea.Cmd {
			if _, live := m.listing.liveAgent(agentID); live {
				return m.enterLive(target)
			}

			return m.enterArchived(target)
		})
		agent.isOff = !found
		entries = append(entries, agent)
	}
	if from != screenWorktrees {
		worktree := openEntry("worktree", m.keys.JumpMenu.Worktree, func(m *model) tea.Cmd {
			return m.enter(screenWorktrees, target)
		})
		worktree.isOff = !scopeAll.listsAgent(m.listing.worktrees, agentID)
		entries = append(entries, worktree)
	}
	if from != screenDocuments {
		documents := openEntry("documents", m.keys.JumpMenu.Documents, func(m *model) tea.Cmd {
			return m.enter(screenDocuments, target)
		})
		documents.isOff = !found || agentRow.Agent.Role == panel.RoleOrchestrator
		entries = append(entries, documents)
	}

	return submenu{kind: formMenu, title: "jump", entries: entries}
}
