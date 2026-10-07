package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// jumpMenu lands on the rows related to an agent, less the screen the menu was opened from.
func jumpMenu(m *model, from screen, agentID string) submenu {
	item, found := m.listing.anyAgent(agentID)
	target := screenTarget{agentID: agentID}

	agent := openEntry("agent", m.keys.JumpMenu.Agent, func(m *model) tea.Cmd {
		if _, live := m.listing.liveAgent(agentID); live {
			return m.enterLive(target)
		}

		return m.enterArchived(target)
	})
	agent.isOff = !found

	worktree := openEntry("worktree", m.keys.JumpMenu.Worktree, func(m *model) tea.Cmd {
		return m.enter(screenWorktrees, target)
	})
	worktree.isOff = !scopeAll.holdsAgent(m.listing.worktrees, agentID)

	documents := openEntry("documents", m.keys.JumpMenu.Documents, func(m *model) tea.Cmd {
		return m.enter(screenDocuments, target)
	})
	documents.isOff = !found || item.Agent.Role == panel.RoleOrchestrator

	var entries []submenuEntry
	for _, jump := range []struct {
		to    screen
		entry submenuEntry
	}{
		{to: screenAgents, entry: agent},
		{to: screenWorktrees, entry: worktree},
		{to: screenDocuments, entry: documents},
	} {
		if jump.to != from {
			entries = append(entries, jump.entry)
		}
	}

	return submenu{kind: formMenu, title: "jump", entries: entries}
}
