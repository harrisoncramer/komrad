package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// jumpMenu is the menu landing on the rows related to an agent, or to the task it names in place
// of the agent, less the screen it was opened from.
func jumpMenu(m *model, from screen, to screenTarget) submenu {
	agentID := to.agentID
	_, found := m.listing.anyAgent(agentID)
	target := screenTarget{agentID: agentID}

	var entries []submenuEntry
	if from != screenAgents {
		if task, listed := m.listing.tasks[to.taskID]; listed {
			entries = append(entries, taskJumpEntry(m, task))
		} else {
			agent := openEntry("agent", m.keys.JumpMenu.Agent, func(m *model) tea.Cmd {
				if _, live := m.listing.liveAgent(agentID); live {
					return m.enterLive(target)
				}

				return m.enterArchived(target)
			})
			agent.isOff = !found
			entries = append(entries, agent)
		}
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

// taskJumpEntry lands on a task's row, off when its writer is not listed.
func taskJumpEntry(m *model, task panel.Task) submenuEntry {
	writer, found := m.listing.findTaskWriter(task)
	entry := openEntry("task", m.keys.JumpMenu.Task, func(m *model) tea.Cmd {
		target := screenTarget{agentID: writer.Agent.ID, taskID: task.ID}
		if task.ArchivedAt != nil {
			return m.enterArchived(target)
		}

		return m.enterLive(target)
	})
	entry.isOff = !found

	return entry
}
