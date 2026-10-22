package ui

import (
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// Jump is the menu landing on the rows related to an agent, or to the task it names in place
// of the agent, less the screen it was opened from.
func (m *model) Jump(from screen.ID, to screen.Target) menu.Menu {
	agentID := to.AgentID
	_, found := m.listing.AnyAgent(agentID)
	target := screen.Target{AgentID: agentID}

	var entries []menu.Entry
	if from != screen.Agents {
		if task, listed := m.listing.Tasks[to.TaskID]; listed {
			entries = append(entries, taskJumpEntry(m, task))
		} else {
			agent := menu.Leaving("agent", m.keys.JumpMenu.Agent, func(in screen.Inputs) screen.Request {
				if _, live := in.Listing.LiveAgent(agentID); live {
					return m.enterLive(target)
				}

				return m.enterArchived(target)
			})
			agent.Off = !found
			entries = append(entries, agent)
		}
	}
	if from != screen.Worktrees {
		worktree := menu.Leaving("worktree", m.keys.JumpMenu.Worktree, func(screen.Inputs) screen.Request {
			return screen.Open{Screen: screen.Worktrees, Target: target}
		})
		worktree.Off = !scopeAll.listsAgent(m.listing.Worktrees, agentID)
		entries = append(entries, worktree)
	}
	if from != screen.Documents {
		documents := menu.Leaving("documents", m.keys.JumpMenu.Documents, func(screen.Inputs) screen.Request {
			return screen.Open{Screen: screen.Documents, Target: target}
		})
		documents.Off = !found
		entries = append(entries, documents)
	}

	return menu.Menu{
		Title:   "jump",
		Entries: entries,
	}
}

// taskJumpEntry lands on a task's row, off when its writer is not listed.
func taskJumpEntry(m *model, task panel.Task) menu.Entry {
	writer, found := m.listing.FindTaskWriter(task)
	entry := menu.Leaving("task", m.keys.JumpMenu.Task, func(screen.Inputs) screen.Request {
		target := screen.Target{AgentID: writer.Agent.ID, TaskID: task.ID}
		if task.ArchivedAt != nil {
			return m.enterArchived(target)
		}

		return m.enterLive(target)
	})
	entry.Off = !found

	return entry
}
