package ui

import (
	"komrad/internal/ui/panel"

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

// taskMenu is the hovered task's band: the pages of the agent it wears, and another agent started on it.
func taskMenu(m *model, row agentRow) submenu {
	menu := agentMenu(m, *row.lead, openEntry("new agent", m.keys.Menu.NewAgent, func(m *model) tea.Cmd {
		return openNewAgentOnTask(m, row.workflow, *row.task)
	}))
	menu.title = row.task.Slug

	return menu
}

// agentEntries are the entries the agents list adds to the band on the hovered agent.
func (a *agentsScreen) agentEntries(m *model, item panel.AgentRow) []submenuEntry {
	entries := append(a.editEntries(m), openEntry("new child", m.keys.Menu.Child, func(m *model) tea.Cmd {
		return openNewChild(m, item.Agent)
	}))

	if a.isRetryableAgent(m) {
		entries = append(entries, openEntry("retry launch", m.keys.Menu.Retry, func(m *model) tea.Cmd {
			return openRetryForm(m, item.Agent)
		}))
	}

	return append(entries, a.snoozeEntries(m, item)...)
}

// snoozeEntries is the entry that snoozes or wakes the agent's family in triage, and none when it
// is not in triage.
func (a *agentsScreen) snoozeEntries(m *model, item panel.AgentRow) []submenuEntry {
	family, ok := a.triage.findFamily(item, m.listing.agents)
	if !ok {
		return nil
	}

	label := "snooze"
	if a.triage.isSnoozed(family.head.Agent.ID) {
		label = "wake"
	}

	return []submenuEntry{{
		label: label,
		keys:  m.keys.Menu.Snooze,
		write: func(m *model) tea.Cmd {
			m.agents.toggleSnooze(m)

			return nil
		},
	}}
}

// editEntries is the entry that opens an editor on the hovered agent's worktree, or closes the
// one already open.
func (a *agentsScreen) editEntries(m *model) []submenuEntry {
	if !a.isEditable(m) {
		return nil
	}

	return []submenuEntry{{
		label: a.editDesc(m),
		keys:  m.keys.Menu.Edit,
		write: func(m *model) tea.Cmd {
			return m.agents.pressEdit(m)
		},
	}}
}

// workflowEntries are the entries on the hovered workflow.
func workflowEntries(m *model, workflow panel.Workflow) []submenuEntry {
	return []submenuEntry{
		openEntry("edit workflow", m.keys.Menu.EditWorkflow, func(m *model) tea.Cmd {
			return editWorkflowForm(m, workflow)
		}),
		openEntry("adopt", m.keys.Menu.Adopt, func(m *model) tea.Cmd { return openAdopt(m, &workflow) }),
	}
}
