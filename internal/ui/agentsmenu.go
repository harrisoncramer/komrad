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
func (a *agentsScreen) buildScreenMenu(m *model, in screen.Inputs) (menu.Menu, bool) {
	return menu.Menu{
		Title: "agents",
		Entries: []menu.Entry{
			menu.Leaving("new workflow", a.deps.keys.Menu.NewWorkflow, func() screen.Request { return a.newWorkflowForm(m) }),
		},
	}, true
}

// taskMenu is the hovered task's band: its pages, what starts or edits work on it, its git, and
// the menus that hide, archive or delete it.
func (a *agentsScreen) taskMenu(m *model, in screen.Inputs, row agentRow) menu.Menu {
	task := *row.task
	writer, _ := row.writer()
	band := menu.Menu{
		Title: task.Slug,
		Entries: slices.Concat(
			[]menu.Entry{a.taskReviewEntry(m, writer.Agent), a.taskLogsEntry(task)},
			subpageEntries(m, a.deps.keys, writer, []subpage{subpageChecks}),
			[]menu.Entry{
				a.taskTerminalEntry(taskDir(task, row.workflow)),
			},
			a.newAgentEntries(m, row.workflow, task),
			[]menu.Entry{
				menu.Leaving("edit task", a.deps.keys.Menu.EditRow, func() screen.Request { return a.openEditTask(task) }),
				a.notesEntry(row),
			},
			a.restartEntries(m, task),
			a.retryEntries(writer),
			a.snoozeEntries(m, in, writer),
			a.editEntries(m, writer),
			[]menu.Entry{
				menu.Nested("git", a.deps.keys.Menu.Git, gitMenu(m, task)),
				menu.Nested("jump", a.deps.keys.Menu.Jump, a.taskJumpMenu(m, writer.Agent.ID, task.ID)),
			},
		),
	}

	band = menu.WithNested(band, "hide", a.deps.keys.Menu.Hide, a.hideMenu(m, []panel.Task{task}))

	return menu.WithNested(band, "archive", a.deps.keys.Menu.Archive, a.taskArchiveMenu(m, task))
}

// newAgentEntries is the entry that starts an agent on a task, and none when no agent can start on it.
func (a *agentsScreen) newAgentEntries(m *model, workflow panel.Workflow, task panel.Task) []menu.Entry {
	if !isTaskStartable(m, task) {
		return nil
	}

	return []menu.Entry{menu.Leaving("new agent", a.deps.keys.Menu.NewAgent, func() screen.Request {
		return a.openNewAgentOnTask(m, workflow, task)
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
func (a *agentsScreen) taskJumpMenu(m *model, agentID, taskID string) menu.Menu {
	band := jumpMenu(m, a.deps.keys, screen.Agents, screen.Target{AgentID: agentID})
	band.Entries = xslice.Map(band.Entries, func(entry menu.Entry) menu.Entry {
		if !slices.Equal(entry.Keys, a.deps.keys.JumpMenu.Documents) {
			return entry
		}

		return menu.Leaving("documents", a.deps.keys.JumpMenu.Documents, func() screen.Request {
			return screen.Open{Screen: screen.Documents, Target: screen.Target{AgentID: agentID, TaskID: taskID}}
		})
	})

	return band
}

// taskLogsEntry opens the runs of every agent on a task.
func (a *agentsScreen) taskLogsEntry(task panel.Task) menu.Entry {
	return menu.Leaving("logs", a.deps.keys.Menu.Logs, func() screen.Request {
		return screen.Open{Screen: screen.Logs, Target: screen.Target{TaskID: task.ID, OpenedFrom: screen.Agents}}
	})
}

// taskTerminalEntry opens a shell in a task's directory, off when it has none.
func (a *agentsScreen) taskTerminalEntry(dir string) menu.Entry {
	entry := menu.Leaving("terminal", a.deps.keys.Menu.Terminal, func() screen.Request {
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
func (a *agentsScreen) taskReviewEntry(m *model, writer panel.Agent) menu.Entry {
	entry := menu.Leaving("review", a.deps.keys.Menu.Review, func() screen.Request {
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
func (a *agentsScreen) agentRowMenu(m *model, in screen.Inputs, item panel.AgentRow) menu.Menu {
	pages := subpageEntries(m, a.deps.keys, item, []subpage{subpageLogs})
	own := append(a.editEntries(m, item), a.retryEntries(item)...)
	if item.Agent.TaskID == "" {
		own = append(own, a.snoozeEntries(m, in, item)...)
	}

	band := menu.Menu{
		Title:   in.Listing.AgentName(item.Agent),
		Entries: slices.Concat(pages, own),
	}
	if item.Agent.Role == panel.RoleWriter {
		return band
	}

	return menu.WithNested(band, "archive", a.deps.keys.Menu.Archive, a.agentArchiveMenu(item.Agent))
}

// retryEntries is the entry that relaunches an agent that failed to create, and none otherwise.
func (a *agentsScreen) retryEntries(item panel.AgentRow) []menu.Entry {
	if item.Agent.State != panel.StateFailedToCreate {
		return nil
	}

	return []menu.Entry{menu.Leaving("retry launch", a.deps.keys.Menu.Retry, func() screen.Request {
		return a.openRetryForm(item.Agent)
	})}
}

// snoozeEntries is the entry that snoozes or wakes the agent's task in triage, and none when it
// is not in triage.
func (a *agentsScreen) snoozeEntries(m *model, in screen.Inputs, item panel.AgentRow) []menu.Entry {
	triaged, ok := a.triage.findTask(item, in.Listing.Agents)
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
		Keys:  a.deps.keys.Menu.Snooze,
		After: menu.Hold,
		Pick:  func() screen.Request { return m.agents.toggleSnooze(m, m.inputs(), key) },
	}}
}

// closeEditEntries is the entry that closes the editor already open, and none while it is shut.
func (a *agentsScreen) closeEditEntries(m *model) []menu.Entry {
	if a.editing == nil {
		return nil
	}

	return []menu.Entry{menu.Holding("close the editor", a.deps.keys.Menu.Edit, func() screen.Request {
		return m.agents.releaseEdit(m.inputs())
	})}
}

// editEntries is the entry that closes the editor already open, or opens one on the agent's worktree.
func (a *agentsScreen) editEntries(m *model, item panel.AgentRow) []menu.Entry {
	if a.editing != nil {
		return a.closeEditEntries(m)
	}

	return []menu.Entry{menu.Holding("edit files", a.deps.keys.Menu.Edit, func() screen.Request {
		return m.agents.pressEdit(m.inputs(), item)
	})}
}

// workflowEntries are the entries on the hovered workflow, and on its orchestrator when it has one.
func (a *agentsScreen) workflowEntries(m *model, workflow panel.Workflow, orchestrator *panel.Agent) []menu.Entry {
	entries := []menu.Entry{
		menu.Leaving("new task", a.deps.keys.Menu.NewAgent, func() screen.Request { return a.openNewTask(&workflow) }),
		menu.Leaving("edit workflow", a.deps.keys.Menu.EditRow, func() screen.Request {
			return a.editWorkflowForm(workflow)
		}),
		menu.Leaving("adopt", a.deps.keys.Menu.Adopt, func() screen.Request { return a.openAdopt(&workflow) }),
	}

	return append(entries, a.orchestratorRestartEntries(m, workflow, orchestrator)...)
}
