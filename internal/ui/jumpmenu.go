package ui

import (
	"komrad/config"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// jumpMenu is the menu landing on the rows related to an agent, or to the task it names in place
// of the agent, less the screen it was opened from.
func jumpMenu(m *model, bindings config.Keybindings, from screen.ID, to screen.Target) menu.Menu {
	agentID := to.AgentID
	_, found := m.listing.AnyAgent(agentID)
	target := screen.Target{AgentID: agentID}

	var entries []menu.Entry
	if from != screen.Agents {
		if task, listed := m.listing.Tasks[to.TaskID]; listed {
			entries = append(entries, taskJumpEntry(m, bindings, task))
		} else {
			agent := menu.Leaving("agent", bindings.JumpMenu.Agent, func() screen.Request {
				if _, live := m.listing.LiveAgent(agentID); live {
					return m.enterLive(target)
				}

				return m.enterArchived(target)
			})
			agent.Off = !found
			entries = append(entries, agent)
		}
	}
	if from != screen.Worktrees {
		worktree := menu.Leaving("worktree", bindings.JumpMenu.Worktree, func() screen.Request {
			return screen.Open{Screen: screen.Worktrees, Target: target}
		})
		worktree.Off = !scopeAll.listsAgent(m.listing.Worktrees, agentID)
		entries = append(entries, worktree)
	}
	if from != screen.Documents {
		documents := menu.Leaving("documents", bindings.JumpMenu.Documents, func() screen.Request {
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
func taskJumpEntry(m *model, bindings config.Keybindings, task panel.Task) menu.Entry {
	writer, found := m.listing.FindTaskWriter(task)
	entry := menu.Leaving("task", bindings.JumpMenu.Task, func() screen.Request {
		target := screen.Target{AgentID: writer.Agent.ID, TaskID: task.ID}
		if task.ArchivedAt != nil {
			return m.enterArchived(target)
		}

		return m.enterLive(target)
	})
	entry.Off = !found

	return entry
}
