package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// buildScreenMenu builds the agents list's own band, for starting an agent or a workflow.
func (a *agentsScreen) buildScreenMenu(m *model) (submenu, bool) {
	in := a.enclosingWorkflow(m)
	if workflow, ok := a.hoveredWorkflow(m); ok {
		in = &workflow
	}

	return submenu{
		kind:  formMenu,
		title: "agents",
		entries: []submenuEntry{
			openEntry("new agent", m.keys.Menu.NewAgent, func(m *model) tea.Cmd { return openNewAgent(m, in) }),
			openEntry("new workflow", m.keys.Menu.NewWorkflow, newWorkflowForm),
		},
	}, true
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

	if family, ok := a.triage.findFamily(item, m.listing.agents); ok {
		label := "snooze"
		if a.triage.isSnoozed(family.head.Agent.ID) {
			label = "wake"
		}

		entries = append(entries, submenuEntry{
			label: label,
			keys:  m.keys.Menu.Snooze,
			write: func(m *model) tea.Cmd {
				m.agents.toggleSnooze(m)

				return nil
			},
		})
	}

	return entries
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
