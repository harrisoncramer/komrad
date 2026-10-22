package ui

import (
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// jumpMenu is the menu landing on the rows related to an agent, or to the task it names in place
// of the agent, less the screen it was opened from.
func jumpMenu(m *model, from screen.ID, to screen.Target) submenu {
	agentID := to.AgentID
	_, found := m.listing.AnyAgent(agentID)
	target := screen.Target{AgentID: agentID}

	var entries []submenuEntry
	if from != screen.Agents {
		if task, listed := m.listing.Tasks[to.TaskID]; listed {
			entries = append(entries, taskJumpEntry(m, task))
		} else {
			agent := openEntry("agent", m.keys.JumpMenu.Agent, func(m *model) tea.Cmd {
				if _, live := m.listing.LiveAgent(agentID); live {
					return m.enterLive(target)
				}

				return m.enterArchived(target)
			})
			agent.isOff = !found
			entries = append(entries, agent)
		}
	}
	if from != screen.Worktrees {
		worktree := openEntry("worktree", m.keys.JumpMenu.Worktree, func(m *model) tea.Cmd {
			return m.enter(screen.Worktrees, target)
		})
		worktree.isOff = !scopeAll.listsAgent(m.listing.Worktrees, agentID)
		entries = append(entries, worktree)
	}
	if from != screen.Documents {
		documents := openEntry("documents", m.keys.JumpMenu.Documents, func(m *model) tea.Cmd {
			return m.enter(screen.Documents, target)
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
	writer, found := m.listing.FindTaskWriter(task)
	entry := openEntry("task", m.keys.JumpMenu.Task, func(m *model) tea.Cmd {
		target := screen.Target{AgentID: writer.Agent.ID, TaskID: task.ID}
		if task.ArchivedAt != nil {
			return m.enterArchived(target)
		}

		return m.enterLive(target)
	})
	entry.isOff = !found

	return entry
}
