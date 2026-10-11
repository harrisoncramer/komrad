package ui

import (
	"slices"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// jumpMenu is the menu landing on the rows related to an agent, less the screen it was opened from.
func jumpMenu(m *model, from screen, agentID string) submenu {
	_, found := m.listing.anyAgent(agentID)
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
		documents.isOff = !found
		entries = append(entries, documents)
	}

	return submenu{
		kind:    formMenu,
		title:   "jump",
		entries: entries,
	}
}

// withTaskJump swaps a jump menu's agent entry for one landing on the task's row, leaving it be
// when the task is not listed.
func withTaskJump(m *model, menu submenu, taskID string) submenu {
	task, listed := m.listing.tasks[taskID]
	if !listed {
		return menu
	}

	menu.entries = xslice.Map(menu.entries, func(entry submenuEntry) submenuEntry {
		if !slices.Equal(entry.keys, m.keys.JumpMenu.Agent) {
			return entry
		}

		return taskJumpEntry(m, task)
	})

	return menu
}

// taskJumpEntry lands on a task's row, off when no agent on it lists the task.
func taskJumpEntry(m *model, task panel.Task) submenuEntry {
	lead, found := m.listing.findTaskLead(task)
	entry := openEntry("task", m.keys.JumpMenu.Task, func(m *model) tea.Cmd {
		target := screenTarget{agentID: lead.Agent.ID, taskID: task.ID}
		if task.ArchivedAt != nil {
			return m.enterArchived(target)
		}

		return m.enterLive(target)
	})
	entry.isOff = !found

	return entry
}
