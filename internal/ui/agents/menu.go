package agents

import (
	"errors"
	"slices"

	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

var errNoTaskDir = errors.New("the task has no checkout, and its workflow has no directory")

// BuildScreenMenu builds the agents list's own band, for starting a workflow, under every row.
func (a *Screen) BuildScreenMenu(_ screen.Inputs, _ menu.Nester) (menu.Menu, bool) {
	return menu.Menu{
		Title: "agents",
		Entries: []menu.Entry{
			menu.Leaving("new workflow", a.deps.Keys.Menu.NewWorkflow, a.newWorkflowForm),
		},
	}, true
}

// taskMenu is the hovered task's band: its pages, what starts or edits work on it, its git, and
// the menus that hide, archive or delete it.
func (a *Screen) taskMenu(in screen.Inputs, nest menu.Nester, row agentRow) menu.Menu {
	task := *row.task
	writer, _ := row.writer()
	band := menu.Menu{
		Title: task.Slug,
		Entries: slices.Concat(
			[]menu.Entry{a.taskReviewEntry(in, writer.Agent), a.taskLogsEntry(task)},
			subpageEntries(in.Listing, a.deps.Keys, writer, []subpage{subpageChecks}),
			[]menu.Entry{
				a.taskTerminalEntry(taskDir(task, row.workflow)),
			},
			a.newAgentEntries(in, row.workflow, task),
			[]menu.Entry{
				menu.Leaving("edit task", a.deps.Keys.Menu.EditRow, func(screen.Inputs) screen.Request { return a.openEditTask(task) }),
				a.notesEntry(row),
			},
			a.restartEntries(in, task),
			a.retryEntries(writer),
			a.snoozeEntries(in, writer),
			a.editEntries(writer),
			[]menu.Entry{
				menu.Nested("git", a.deps.Keys.Menu.Git, nest.Git(task)),
				menu.Nested("jump", a.deps.Keys.Menu.Jump, a.taskJumpMenu(nest, writer.Agent.ID, task.ID)),
			},
		),
	}

	band = menu.WithNested(band, "hide", a.deps.Keys.Menu.Hide, a.hideMenu([]panel.Task{task}))

	return menu.WithNested(band, "archive", a.deps.Keys.Menu.Archive, a.taskArchiveMenu(task))
}

// newAgentEntries is the entry that starts an agent on a task, and none when no agent can start on it.
func (a *Screen) newAgentEntries(in screen.Inputs, workflow panel.Workflow, task panel.Task) []menu.Entry {
	if !isTaskStartable(in.Listing, task) {
		return nil
	}

	return []menu.Entry{menu.Leaving("new agent", a.deps.Keys.Menu.NewAgent, func(in screen.Inputs) screen.Request {
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
func (a *Screen) taskJumpMenu(nest menu.Nester, agentID, taskID string) menu.Menu {
	band := nest.Jump(screen.Agents, screen.Target{AgentID: agentID})
	band.Entries = xslice.Map(band.Entries, func(entry menu.Entry) menu.Entry {
		if !slices.Equal(entry.Keys, a.deps.Keys.JumpMenu.Documents) {
			return entry
		}

		return menu.Leaving("documents", a.deps.Keys.JumpMenu.Documents, func(screen.Inputs) screen.Request {
			return screen.Open{Screen: screen.Documents, Target: screen.Target{AgentID: agentID, TaskID: taskID}}
		})
	})

	return band
}

// taskLogsEntry opens the runs of every agent on a task.
func (a *Screen) taskLogsEntry(task panel.Task) menu.Entry {
	return menu.Leaving("logs", a.deps.Keys.Menu.Logs, func(screen.Inputs) screen.Request {
		return screen.Open{Screen: screen.Logs, Target: screen.Target{TaskID: task.ID, OpenedFrom: screen.Agents}}
	})
}

// taskTerminalEntry opens a shell in a task's directory, off when it has none.
func (a *Screen) taskTerminalEntry(dir string) menu.Entry {
	entry := menu.Leaving("terminal", a.deps.Keys.Menu.Terminal, func(screen.Inputs) screen.Request {
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
func (a *Screen) taskReviewEntry(in screen.Inputs, writer panel.Agent) menu.Entry {
	entry := menu.Leaving("review", a.deps.Keys.Menu.Review, func(in screen.Inputs) screen.Request {
		if !in.Listing.IsReviewable(writer) {
			return refuseSubpage("Could not open the review of "+in.Listing.Tasks.FindForAgent(writer).Slug, panel.ErrNoCheckout)
		}

		return screen.Open{Screen: screen.Review, Target: screen.Target{AgentID: writer.ID}}
	})
	entry.Off = !in.Listing.IsReviewable(writer)

	return entry
}

// agentRowMenu is the hovered agent's band, which holds what concerns its session and leaves the
// work to its task's row.
func (a *Screen) agentRowMenu(in screen.Inputs, item panel.AgentRow) menu.Menu {
	pages := subpageEntries(in.Listing, a.deps.Keys, item, []subpage{subpageLogs})
	own := append(a.editEntries(item), a.retryEntries(item)...)
	if item.Agent.TaskID == "" {
		own = append(own, a.snoozeEntries(in, item)...)
	}

	band := menu.Menu{
		Title:   in.Listing.AgentName(item.Agent),
		Entries: slices.Concat(pages, own),
	}
	if item.Agent.Role == agentv1.AgentRole_AGENT_ROLE_WRITER {
		return band
	}

	return menu.WithNested(band, "archive", a.deps.Keys.Menu.Archive, a.agentArchiveMenu(item.Agent))
}

// retryEntries is the entry that relaunches an agent that failed to create, and none otherwise.
func (a *Screen) retryEntries(item panel.AgentRow) []menu.Entry {
	if item.Agent.State != agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE {
		return nil
	}

	return []menu.Entry{menu.Leaving("retry launch", a.deps.Keys.Menu.Retry, func(screen.Inputs) screen.Request {
		return a.openRetryForm(item.Agent)
	})}
}

// snoozeEntries is the entry that snoozes or wakes the agent's task in triage, and none when it
// is not in triage.
func (a *Screen) snoozeEntries(in screen.Inputs, item panel.AgentRow) []menu.Entry {
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
		Keys:  a.deps.Keys.Menu.Snooze,
		After: menu.Hold,
		Pick:  func(in screen.Inputs) screen.Request { return a.toggleSnooze(in, key) },
	}}
}

// closeEditEntries is the entry that closes the editor already open, and none while it is shut.
func (a *Screen) closeEditEntries() []menu.Entry {
	if a.editing == nil {
		return nil
	}

	return []menu.Entry{menu.Holding("close the editor", a.deps.Keys.Menu.Edit, func(screen.Inputs) screen.Request {
		return a.closeEdit()
	})}
}

// editEntries is the entry that closes the editor already open, or opens one on the agent's worktree.
func (a *Screen) editEntries(item panel.AgentRow) []menu.Entry {
	if a.editing != nil {
		return a.closeEditEntries()
	}

	return []menu.Entry{menu.Holding("edit files", a.deps.Keys.Menu.Edit, func(in screen.Inputs) screen.Request {
		return a.pressEdit(in, item)
	})}
}

// workflowEntries are the entries on the hovered workflow, and on its orchestrator when it has one.
func (a *Screen) workflowEntries(workflow panel.Workflow, orchestrator *panel.Agent) []menu.Entry {
	entries := []menu.Entry{
		menu.Leaving("new task", a.deps.Keys.Menu.NewAgent, func(screen.Inputs) screen.Request { return a.openNewTask(&workflow) }),
		menu.Leaving("edit workflow", a.deps.Keys.Menu.EditRow, func(screen.Inputs) screen.Request {
			return a.editWorkflowForm(workflow)
		}),
		menu.Leaving("adopt", a.deps.Keys.Menu.Adopt, func(screen.Inputs) screen.Request { return a.openAdopt(&workflow) }),
	}

	return append(entries, a.orchestratorRestartEntries(workflow, orchestrator)...)
}

// BuildItemMenu builds the band on the hovered row or the selection.
func (a *Screen) BuildItemMenu(in screen.Inputs, nest menu.Nester) (menu.Menu, bool) {
	if a.list().IsSelecting() {
		chosen := chosenAgents(a.Selected, a.rows(in))
		if len(chosen) == 0 {
			return menu.Menu{}, false
		}

		work := splitChosen(in.Listing, chosen)
		hideable := work.wholeTasks
		if len(work.loneAgents) > 0 {
			hideable = nil
		}
		band := menu.WithNested(rowMenu(describeAgents(chosen)), "hide", a.deps.Keys.Menu.Hide, a.hideMenu(hideable))

		return menu.WithNested(band, "archive", a.deps.Keys.Menu.Archive, a.chosenArchiveMenu(work)), true
	}

	if !a.isBrowsing() {
		return menu.Menu{}, false
	}

	row, ok := a.hoveredRow(in)
	if !ok {
		return menu.Menu{}, false
	}
	if task, ok := a.taskRowOf(in, row); ok {
		return a.taskMenu(in, nest, task), true
	}

	item, hasAgent := row.paneItem()

	if workflow, ok := a.hoveredWorkflow(in); ok {
		band := rowMenu(workflow.Name)
		var orchestrator *panel.Agent
		if hasAgent {
			band = agentMenu(agentMenuParams{
				in:       in,
				nest:     nest,
				bindings: a.deps.Keys,
				item:     item,
				own:      slices.Concat(a.closeEditEntries(), a.snoozeEntries(in, item)),
			})
			orchestrator = &item.Agent
		}
		band.Entries = append(band.Entries, a.workflowEntries(workflow, orchestrator)...)

		band = menu.WithNested(band, "hide", a.deps.Keys.Menu.Hide, a.hideMenu(nil))

		return menu.WithNested(band, "archive", a.deps.Keys.Menu.Archive, a.workflowArchiveMenu(workflow)), true
	}

	if !hasAgent {
		return menu.Menu{}, false
	}

	return a.agentRowMenu(in, item), true
}

// rowMenu is a band titled for a row that has no entries of its own.
func rowMenu(name string) menu.Menu {
	return menu.Menu{
		Title: name,
	}
}
