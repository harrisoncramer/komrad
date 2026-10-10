package ui

import (
	"slices"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// buildScreenMenu builds the agents list's own band, for starting a task or a workflow, which a
// task's row and an agent's leave out.
func (a *agentsScreen) buildScreenMenu(m *model) (submenu, bool) {
	if row, ok := a.hoveredRow(m); ok && (row.isTask() || !row.header) {
		return submenu{}, false
	}

	in := a.enclosingWorkflow(m)
	if workflow, ok := a.hoveredWorkflow(m); ok {
		in = &workflow
	}

	return submenu{
		kind:  formMenu,
		title: "agents",
		entries: []submenuEntry{
			openEntry("new task", m.keys.Menu.NewAgent, func(m *model) tea.Cmd { return openNewTask(m, in) }),
			openEntry("new workflow", m.keys.Menu.NewWorkflow, newWorkflowForm),
		},
	}, true
}

// taskMenu is the hovered task's band: the pages of its lead, what starts or edits work on it,
// and the menus that hide, archive or delete it.
func (a *agentsScreen) taskMenu(m *model, row agentRow) submenu {
	lead, task := *row.lead, *row.task
	writer := m.listing.findTaskWriter(task, lead)
	own := slices.Concat(
		newAgentEntries(m, row.workflow, task),
		[]submenuEntry{
			openEntry("edit task", m.keys.Menu.EditRow, func(m *model) tea.Cmd { return openEditTask(m, task) }),
			handOffEntry(m, task),
		},
		a.snoozeEntries(m, lead),
		a.editEntries(m, writer),
	)

	menu := agentMenu(m, lead, own...)
	menu.title = task.Slug
	menu.entries = xslice.Map(menu.entries, func(entry submenuEntry) submenuEntry {
		switch {
		case slices.Equal(entry.keys, m.keys.Menu.Review):
			return taskReviewEntry(m, lead.Agent)
		case slices.Equal(entry.keys, m.keys.Menu.Logs):
			return taskLogsEntry(m, task)
		case slices.Equal(entry.keys, m.keys.Menu.Jump):
			return nestedEntry("jump", m.keys.Menu.Jump, taskJumpMenu(m, row))
		}

		return entry
	})

	tasks := []panel.Task{task}
	menu = withNested(menu, "hide", m.keys.Menu.Hide, a.hideMenu(m, tasks))

	return withNested(menu, "archive", m.keys.Menu.Archive, tasksArchiveMenu(m, tasks, false))
}

// newAgentEntries is the entry that starts an agent on a task, and none once its worktree is gone.
func newAgentEntries(m *model, workflow panel.Workflow, task panel.Task) []submenuEntry {
	if task.WorktreeRemoved {
		return nil
	}

	return []submenuEntry{openEntry("new agent", m.keys.Menu.NewAgent, func(m *model) tea.Cmd {
		return openNewAgentOnTask(m, workflow, task)
	})}
}

// taskJumpMenu is the jump menu of the agent a task wears, its documents entry landing on the task's.
func taskJumpMenu(m *model, row agentRow) submenu {
	menu := jumpMenu(m, screenAgents, row.lead.Agent.ID)
	menu.entries = xslice.Map(menu.entries, func(entry submenuEntry) submenuEntry {
		if !slices.Equal(entry.keys, m.keys.JumpMenu.Documents) {
			return entry
		}

		return openEntry("documents", m.keys.JumpMenu.Documents, func(m *model) tea.Cmd {
			return m.enter(screenDocuments, screenTarget{agentID: row.lead.Agent.ID, taskID: row.task.ID})
		})
	})

	return menu
}

// taskLogsEntry opens the runs of every agent on a task.
func taskLogsEntry(m *model, task panel.Task) submenuEntry {
	return openEntry("logs", m.keys.Menu.Logs, func(m *model) tea.Cmd {
		m.leaveForm()

		return m.enter(screenLogs, screenTarget{taskID: task.ID, openedFrom: screenAgents})
	})
}

// taskReviewEntry opens the review of a task through the agent it wears, archived or not.
func taskReviewEntry(m *model, lead panel.Agent) submenuEntry {
	entry := openEntry("review", m.keys.Menu.Review, func(m *model) tea.Cmd {
		if !isReviewable(lead) {
			return m.refuseSubpage("Could not open the review of "+m.listing.findTask(lead).Slug, errReviewNoCheckout)
		}

		return m.enterReview(lead)
	})
	entry.isOff = !isReviewable(lead)

	return entry
}

// agentRowMenu is the hovered agent's band, which holds what concerns its session and leaves the
// work to its task's row.
func (a *agentsScreen) agentRowMenu(m *model, item panel.AgentRow) submenu {
	pages := subpageEntries(m, item, []subpage{subpageLogs})
	own := a.editEntries(m, item)
	if item.Agent.State == panel.StateFailedToCreate {
		own = append(own, openEntry("retry launch", m.keys.Menu.Retry, func(m *model) tea.Cmd {
			return openRetryForm(m, item.Agent)
		}))
	}
	if item.Agent.TaskID == "" {
		own = append(own, a.snoozeEntries(m, item)...)
	}

	menu := submenu{
		kind:    formMenu,
		title:   m.listing.agentName(item.Agent),
		entries: slices.Concat(pages, own),
	}

	return withNested(menu, "archive", m.keys.Menu.Archive, agentArchiveMenu(m, item.Agent))
}

// snoozeEntries is the entry that snoozes or wakes the agent's task in triage, and none when it
// is not in triage.
func (a *agentsScreen) snoozeEntries(m *model, item panel.AgentRow) []submenuEntry {
	family, ok := a.triage.findFamily(item, m.listing.agents)
	if !ok {
		return nil
	}

	key := triageKey(family.head)
	label := "snooze"
	if a.triage.isSnoozed(key) {
		label = "wake"
	}

	return []submenuEntry{{
		label: label,
		keys:  m.keys.Menu.Snooze,
		write: func(m *model) tea.Cmd {
			m.agents.toggleSnooze(m, key)

			return nil
		},
	}}
}

// closeEditEntries is the entry that closes the editor already open, and none while it is shut.
func (a *agentsScreen) closeEditEntries(m *model) []submenuEntry {
	if a.editing == nil {
		return nil
	}

	return []submenuEntry{writeEntry("close the editor", m.keys.Menu.Edit, func(m *model) tea.Cmd {
		return m.agents.releaseEdit(m)
	})}
}

// editEntries is the entry that closes the editor already open, or opens one on the agent's worktree.
func (a *agentsScreen) editEntries(m *model, item panel.AgentRow) []submenuEntry {
	if a.editing != nil {
		return a.closeEditEntries(m)
	}

	return []submenuEntry{writeEntry("edit files", m.keys.Menu.Edit, func(m *model) tea.Cmd {
		return m.agents.pressEdit(m, item)
	})}
}

// workflowEntries are the entries on the hovered workflow.
func workflowEntries(m *model, workflow panel.Workflow) []submenuEntry {
	return []submenuEntry{
		openEntry("edit workflow", m.keys.Menu.EditRow, func(m *model) tea.Cmd {
			return editWorkflowForm(m, workflow)
		}),
		openEntry("adopt", m.keys.Menu.Adopt, func(m *model) tea.Cmd { return openAdopt(m, &workflow) }),
	}
}
