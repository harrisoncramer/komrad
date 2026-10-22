package ui

import (
	"slices"

	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
)

// BuildScreenMenu builds the agents list's own band, for starting a workflow, under every row.
func (a *agentsScreen) BuildScreenMenu(_ screen.Inputs, _ menu.Nester) (menu.Menu, bool) {
	return menu.Menu{
		Title: "agents",
		Entries: []menu.Entry{
			menu.Leaving("new workflow", a.deps.keys.Menu.NewWorkflow, a.newWorkflowForm),
		},
	}, true
}

// taskMenu is the hovered task's band: its pages, what starts or edits work on it, its git, and
// the menus that hide, archive or delete it.
func (a *agentsScreen) taskMenu(in screen.Inputs, nest menu.Nester, row agentRow) menu.Menu {
	task := *row.task
	writer, _ := row.writer()
	band := menu.Menu{
		Title: task.Slug,
		Entries: slices.Concat(
			[]menu.Entry{a.taskReviewEntry(in, writer.Agent), a.taskLogsEntry(task)},
			subpageEntries(in.Listing, a.deps.keys, writer, []subpage{subpageChecks}),
			[]menu.Entry{
				a.taskTerminalEntry(taskDir(task, row.workflow)),
			},
			a.newAgentEntries(in, row.workflow, task),
			[]menu.Entry{
				menu.Leaving("edit task", a.deps.keys.Menu.EditRow, func(screen.Inputs) screen.Request { return a.openEditTask(task) }),
				a.notesEntry(row),
			},
			a.restartEntries(in, task),
			a.retryEntries(writer),
			a.snoozeEntries(in, writer),
			a.editEntries(writer),
			[]menu.Entry{
				menu.Nested("git", a.deps.keys.Menu.Git, nest.Git(task)),
				menu.Nested("jump", a.deps.keys.Menu.Jump, a.taskJumpMenu(nest, writer.Agent.ID, task.ID)),
			},
		),
	}

	band = menu.WithNested(band, "hide", a.deps.keys.Menu.Hide, a.hideMenu([]panel.Task{task}))

	return menu.WithNested(band, "archive", a.deps.keys.Menu.Archive, a.taskArchiveMenu(task))
}

// newAgentEntries is the entry that starts an agent on a task, and none when no agent can start on it.
func (a *agentsScreen) newAgentEntries(in screen.Inputs, workflow panel.Workflow, task panel.Task) []menu.Entry {
	if !isTaskStartable(in.Listing, task) {
		return nil
	}

	return []menu.Entry{menu.Leaving("new agent", a.deps.keys.Menu.NewAgent, func(in screen.Inputs) screen.Request {
		return a.openNewAgentOnTask(in, workflow, task)
	})}
}

// isTaskStartable reports whether an agent can start on a task, which it cannot once the task's
// worktree is gone, once the task has left the listing as one being deleted does, or once it is
// archived with no checkout to join.
func isTaskStartable(l panel.Listing, task panel.Task) bool {
	if _, listed := l.Tasks[task.ID]; !listed {
		return false
	}

	return !task.IsWorktreeRemoved() && (task.ArchivedAt == nil || task.Cwd != "")
}

// taskJumpMenu is the jump menu of the agent that opens a task, with its documents entry landing on the
// task's.
func (a *agentsScreen) taskJumpMenu(nest menu.Nester, agentID, taskID string) menu.Menu {
	band := nest.Jump(screen.Agents, screen.Target{AgentID: agentID})
	band.Entries = xslice.Map(band.Entries, func(entry menu.Entry) menu.Entry {
		if !slices.Equal(entry.Keys, a.deps.keys.JumpMenu.Documents) {
			return entry
		}

		return menu.Leaving("documents", a.deps.keys.JumpMenu.Documents, func(screen.Inputs) screen.Request {
			return screen.Open{Screen: screen.Documents, Target: screen.Target{AgentID: agentID, TaskID: taskID}}
		})
	})

	return band
}

// taskLogsEntry opens the runs of every agent on a task.
func (a *agentsScreen) taskLogsEntry(task panel.Task) menu.Entry {
	return menu.Leaving("logs", a.deps.keys.Menu.Logs, func(screen.Inputs) screen.Request {
		return screen.Open{Screen: screen.Logs, Target: screen.Target{TaskID: task.ID, OpenedFrom: screen.Agents}}
	})
}

// taskTerminalEntry opens a shell in a task's directory, off when it has none.
func (a *agentsScreen) taskTerminalEntry(dir string) menu.Entry {
	entry := menu.Leaving("terminal", a.deps.keys.Menu.Terminal, func(screen.Inputs) screen.Request {
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
func (a *agentsScreen) taskReviewEntry(in screen.Inputs, writer panel.Agent) menu.Entry {
	entry := menu.Leaving("review", a.deps.keys.Menu.Review, func(in screen.Inputs) screen.Request {
		if !in.Listing.IsReviewable(writer) {
			return refuseSubpage("Could not open the review of "+in.Listing.Tasks.FindForAgent(writer).Slug, errReviewNoCheckout)
		}

		return screen.Open{Screen: screen.Review, Target: screen.Target{AgentID: writer.ID}}
	})
	entry.Off = !in.Listing.IsReviewable(writer)

	return entry
}

// agentRowMenu is the hovered agent's band, which holds what concerns its session and leaves the
// work to its task's row.
func (a *agentsScreen) agentRowMenu(in screen.Inputs, item panel.AgentRow) menu.Menu {
	pages := subpageEntries(in.Listing, a.deps.keys, item, []subpage{subpageLogs})
	own := append(a.editEntries(item), a.retryEntries(item)...)
	if item.Agent.TaskID == "" {
		own = append(own, a.snoozeEntries(in, item)...)
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

	return []menu.Entry{menu.Leaving("retry launch", a.deps.keys.Menu.Retry, func(screen.Inputs) screen.Request {
		return a.openRetryForm(item.Agent)
	})}
}

// snoozeEntries is the entry that snoozes or wakes the agent's task in triage, and none when it
// is not in triage.
func (a *agentsScreen) snoozeEntries(in screen.Inputs, item panel.AgentRow) []menu.Entry {
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
		Pick:  func(in screen.Inputs) screen.Request { return a.toggleSnooze(in, key) },
	}}
}

// closeEditEntries is the entry that closes the editor already open, and none while it is shut.
func (a *agentsScreen) closeEditEntries() []menu.Entry {
	if a.editing == nil {
		return nil
	}

	return []menu.Entry{menu.Holding("close the editor", a.deps.keys.Menu.Edit, func(screen.Inputs) screen.Request {
		return a.closeEdit()
	})}
}

// editEntries is the entry that closes the editor already open, or opens one on the agent's worktree.
func (a *agentsScreen) editEntries(item panel.AgentRow) []menu.Entry {
	if a.editing != nil {
		return a.closeEditEntries()
	}

	return []menu.Entry{menu.Holding("edit files", a.deps.keys.Menu.Edit, func(in screen.Inputs) screen.Request {
		return a.pressEdit(in, item)
	})}
}

// workflowEntries are the entries on the hovered workflow, and on its orchestrator when it has one.
func (a *agentsScreen) workflowEntries(workflow panel.Workflow, orchestrator *panel.Agent) []menu.Entry {
	entries := []menu.Entry{
		menu.Leaving("new task", a.deps.keys.Menu.NewAgent, func(screen.Inputs) screen.Request { return a.openNewTask(&workflow) }),
		menu.Leaving("edit workflow", a.deps.keys.Menu.EditRow, func(screen.Inputs) screen.Request {
			return a.editWorkflowForm(workflow)
		}),
		menu.Leaving("adopt", a.deps.keys.Menu.Adopt, func(screen.Inputs) screen.Request { return a.openAdopt(&workflow) }),
	}

	return append(entries, a.orchestratorRestartEntries(workflow, orchestrator)...)
}
