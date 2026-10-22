package ui

import (
	"slices"

	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
)

// buildScreenMenu builds the agents list's own band, for starting a workflow, under every row.
func (a *agentsScreen) buildScreenMenu(m *model) (menu.Menu, bool) {
	return menu.Menu{
		Title: "agents",
		Entries: []menu.Entry{
			menu.Leaving("new workflow", m.keys.Menu.NewWorkflow, func() screen.Request { return newWorkflowForm(m) }),
		},
	}, true
}

// taskMenu is the hovered task's band: its pages, what starts or edits work on it, its git, and
// the menus that hide, archive or delete it.
func (a *agentsScreen) taskMenu(m *model, row agentRow) menu.Menu {
	task := *row.task
	writer, _ := row.writer()
	band := menu.Menu{
		Title: task.Slug,
		Entries: slices.Concat(
			[]menu.Entry{taskReviewEntry(m, writer.Agent), taskLogsEntry(m, task)},
			subpageEntries(m, writer, []subpage{subpageChecks}),
			[]menu.Entry{
				taskTerminalEntry(m, taskDir(task, row.workflow)),
			},
			newAgentEntries(m, row.workflow, task),
			[]menu.Entry{
				menu.Leaving("edit task", m.keys.Menu.EditRow, func() screen.Request { return openEditTask(m, task) }),
				a.notesEntry(m, row),
			},
			restartEntries(m, task),
			retryEntries(m, writer),
			a.snoozeEntries(m, writer),
			a.editEntries(m, writer),
			[]menu.Entry{
				menu.Nested("git", m.keys.Menu.Git, gitMenu(m, task)),
				menu.Nested("jump", m.keys.Menu.Jump, taskJumpMenu(m, writer.Agent.ID, task.ID)),
			},
		),
	}

	band = menu.WithNested(band, "hide", m.keys.Menu.Hide, a.hideMenu(m, []panel.Task{task}))

	return menu.WithNested(band, "archive", m.keys.Menu.Archive, taskArchiveMenu(m, task))
}

// newAgentEntries is the entry that starts an agent on a task, and none when no agent can start on it.
func newAgentEntries(m *model, workflow panel.Workflow, task panel.Task) []menu.Entry {
	if !isTaskStartable(m, task) {
		return nil
	}

	return []menu.Entry{menu.Leaving("new agent", m.keys.Menu.NewAgent, func() screen.Request {
		return openNewAgentOnTask(m, workflow, task)
	})}
}

// isTaskStartable reports whether an agent can start on a task, which it cannot once the task's
// worktree is gone, once the task has left the listing as one being deleted does, or once it is
// archived with no checkout to join.
func isTaskStartable(m *model, task panel.Task) bool {
	if _, listed := m.listing.Tasks[task.ID]; !listed {
		return false
	}

	return !task.IsWorktreeRemoved() && (task.ArchivedAt == nil || task.Cwd != "")
}

// taskJumpMenu is the jump menu of the agent that opens a task, with its documents entry landing on the
// task's.
func taskJumpMenu(m *model, agentID, taskID string) menu.Menu {
	band := jumpMenu(m, screen.Agents, screen.Target{AgentID: agentID})
	band.Entries = xslice.Map(band.Entries, func(entry menu.Entry) menu.Entry {
		if !slices.Equal(entry.Keys, m.keys.JumpMenu.Documents) {
			return entry
		}

		return menu.Leaving("documents", m.keys.JumpMenu.Documents, func() screen.Request {
			return screen.Open{Screen: screen.Documents, Target: screen.Target{AgentID: agentID, TaskID: taskID}}
		})
	})

	return band
}

// taskLogsEntry opens the runs of every agent on a task.
func taskLogsEntry(m *model, task panel.Task) menu.Entry {
	return menu.Leaving("logs", m.keys.Menu.Logs, func() screen.Request {
		return screen.Open{Screen: screen.Logs, Target: screen.Target{TaskID: task.ID, OpenedFrom: screen.Agents}}
	})
}

// taskTerminalEntry opens a shell in a task's directory, off when it has none.
func taskTerminalEntry(m *model, dir string) menu.Entry {
	entry := menu.Leaving("terminal", m.keys.Menu.Terminal, func() screen.Request {
		if dir == "" {
			return refuseSubpage("Could not open a terminal", errNoTaskDir)
		}

		return screen.Batch{form.Close{}, screen.OpenTerminal{Dir: dir}}
	})
	entry.Off = dir == ""

	return entry
}

// taskDir is the task's worktree, or its workflow's directory when it has none.
func taskDir(task panel.Task, workflow panel.Workflow) string {
	if task.Cwd != "" && !task.IsWorktreeRemoved() {
		return task.Cwd
	}

	return workflow.Cwd
}

// taskReviewEntry opens the review of a task through the agent it wears, archived or not.
func taskReviewEntry(m *model, writer panel.Agent) menu.Entry {
	entry := menu.Leaving("review", m.keys.Menu.Review, func() screen.Request {
		if !m.listing.IsReviewable(writer) {
			return refuseSubpage("Could not open the review of "+m.listing.Tasks.FindForAgent(writer).Slug, errReviewNoCheckout)
		}

		return screen.Open{Screen: screen.Review, Target: screen.Target{AgentID: writer.ID}}
	})
	entry.Off = !m.listing.IsReviewable(writer)

	return entry
}

// agentRowMenu is the hovered agent's band, which holds what concerns its session and leaves the
// work to its task's row.
func (a *agentsScreen) agentRowMenu(m *model, item panel.AgentRow) menu.Menu {
	pages := subpageEntries(m, item, []subpage{subpageLogs})
	own := append(a.editEntries(m, item), retryEntries(m, item)...)
	if item.Agent.TaskID == "" {
		own = append(own, a.snoozeEntries(m, item)...)
	}

	band := menu.Menu{
		Title:   m.listing.AgentName(item.Agent),
		Entries: slices.Concat(pages, own),
	}
	if item.Agent.Role == panel.RoleWriter {
		return band
	}

	return menu.WithNested(band, "archive", m.keys.Menu.Archive, agentArchiveMenu(m, item.Agent))
}

// retryEntries is the entry that relaunches an agent that failed to create, and none otherwise.
func retryEntries(m *model, item panel.AgentRow) []menu.Entry {
	if item.Agent.State != panel.StateFailedToCreate {
		return nil
	}

	return []menu.Entry{menu.Leaving("retry launch", m.keys.Menu.Retry, func() screen.Request {
		return openRetryForm(m, item.Agent)
	})}
}

// snoozeEntries is the entry that snoozes or wakes the agent's task in triage, and none when it
// is not in triage.
func (a *agentsScreen) snoozeEntries(m *model, item panel.AgentRow) []menu.Entry {
	triaged, ok := a.triage.findTask(item, m.listing.Agents)
	if !ok {
		return nil
	}

	key := triageKey(triaged.head)
	label := "snooze"
	if a.triage.isSnoozed(key) {
		label = "wake"
	}

	return []menu.Entry{{
		Label: label,
		Keys:  m.keys.Menu.Snooze,
		After: menu.Hold,
		Pick:  func() screen.Request { return m.agents.toggleSnooze(m, key) },
	}}
}

// closeEditEntries is the entry that closes the editor already open, and none while it is shut.
func (a *agentsScreen) closeEditEntries(m *model) []menu.Entry {
	if a.editing == nil {
		return nil
	}

	return []menu.Entry{menu.Holding("close the editor", m.keys.Menu.Edit, func() screen.Request {
		return m.agents.releaseEdit(m)
	})}
}

// editEntries is the entry that closes the editor already open, or opens one on the agent's worktree.
func (a *agentsScreen) editEntries(m *model, item panel.AgentRow) []menu.Entry {
	if a.editing != nil {
		return a.closeEditEntries(m)
	}

	return []menu.Entry{menu.Holding("edit files", m.keys.Menu.Edit, func() screen.Request {
		return m.agents.pressEdit(m, item)
	})}
}

// workflowEntries are the entries on the hovered workflow, and on its orchestrator when it has one.
func workflowEntries(m *model, workflow panel.Workflow, orchestrator *panel.Agent) []menu.Entry {
	entries := []menu.Entry{
		menu.Leaving("new task", m.keys.Menu.NewAgent, func() screen.Request { return openNewTask(m, &workflow) }),
		menu.Leaving("edit workflow", m.keys.Menu.EditRow, func() screen.Request {
			return editWorkflowForm(m, workflow)
		}),
		menu.Leaving("adopt", m.keys.Menu.Adopt, func() screen.Request { return openAdopt(m, &workflow) }),
	}

	return append(entries, orchestratorRestartEntries(m, workflow, orchestrator)...)
}
