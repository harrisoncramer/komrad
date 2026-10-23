package ui

import (
	"slices"

	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// Jump is the menu landing on the rows related to an agent, or to the task it names in place
// of the agent, less the screen it was opened from.
func (m *model) Jump(fromScreen screen.ID, target screen.Target) menu.Menu {
	agentID := target.AgentID
	_, found := m.listing.AnyAgent(agentID)
	agentTarget := screen.Target{AgentID: agentID}

	var entries []menu.Entry
	if fromScreen != screen.Agents {
		if task, listed := m.listing.Tasks[target.TaskID]; listed {
			entries = append(entries, taskJumpEntry(m, task))
		} else {
			agent := menu.Leaving("agent", m.keys.JumpMenu.Agent, func(in screen.Inputs) screen.Request {
				if _, live := in.Listing.LiveAgent(agentID); live {
					return m.enterLive(agentTarget)
				}

				return m.enterArchived(agentTarget)
			})
			agent.IsDimmed = !found
			entries = append(entries, agent)
		}
	}
	if fromScreen != screen.Worktrees {
		worktree := menu.Leaving("worktree", m.keys.JumpMenu.Worktree, func(screen.Inputs) screen.Request {
			return screen.Open{Screen: screen.Worktrees, Target: agentTarget}
		})
		worktree.IsDimmed = !slices.ContainsFunc(m.listing.Worktrees, func(w panel.Worktree) bool { return w.HoldsAgent(agentID) })
		entries = append(entries, worktree)
	}
	if fromScreen != screen.Documents {
		documents := menu.Leaving("documents", m.keys.JumpMenu.Documents, func(screen.Inputs) screen.Request {
			return screen.Open{Screen: screen.Documents, Target: agentTarget}
		})
		documents.IsDimmed = !found
		entries = append(entries, documents)
	}

	return menu.Menu{
		Title:   "jump",
		Entries: entries,
	}
}

// taskJumpEntry lands on a task's row, dimmed when its writer is not listed.
func taskJumpEntry(m *model, task panel.Task) menu.Entry {
	writer, found := m.listing.FindTaskWriter(task)
	entry := menu.Leaving("task", m.keys.JumpMenu.Task, func(screen.Inputs) screen.Request {
		target := screen.Target{AgentID: writer.Agent.ID, TaskID: task.ID}
		if task.ArchivedAt != nil {
			return m.enterArchived(target)
		}

		return m.enterLive(target)
	})
	entry.IsDimmed = !found

	return entry
}

// enterArchived lands the agents view on its archived kind, or its deleted kind when that lists the
// target.
func (m *model) enterArchived(target screen.Target) screen.Request {
	m.agents.ShowArchived(m.listing, target)

	return screen.Open{Screen: screen.Agents, Target: target}
}

// enterLive lands the agents view on the live kind.
func (m *model) enterLive(target screen.Target) screen.Request {
	m.agents.ShowLive()

	return screen.Open{Screen: screen.Agents, Target: target}
}
