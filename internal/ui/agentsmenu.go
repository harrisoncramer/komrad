package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// screenMenued is a screen with a band of its own between the hovered row's and the panel's.
type screenMenued interface {
	screenMenu(s panelShared) (submenu, bool)
}

// screenMenu is the agents list's own band, for starting an agent or a workflow.
func (a *agentsScreen) screenMenu(s panelShared) (submenu, bool) {
	in := a.enclosingWorkflow(s)
	if workflow, ok := a.hoveredWorkflow(s); ok {
		in = &workflow
	}

	return submenu{
		kind:  formMenu,
		title: "agents",
		crumb: "agents",
		entries: []submenuEntry{
			openEntry("new agent", s.keys.Menu.NewAgent, func(h host, _ panelShared) tea.Cmd { return openNewAgent(h, in) }),
			openEntry("new workflow", s.keys.Menu.NewWorkflow, newWorkflowForm),
		},
	}, true
}

// agentEntries are the entries the agents list adds to the band on the hovered agent.
func (a *agentsScreen) agentEntries(s panelShared, item panel.AgentRow) []submenuEntry {
	entries := append(a.editEntries(s), openEntry("new child", s.keys.Menu.Child, func(h host, _ panelShared) tea.Cmd {
		return openNewChild(h, item.Agent)
	}))

	if a.isRetryableAgent(s) {
		entries = append(entries, openEntry("retry launch", s.keys.Menu.Retry, func(h host, _ panelShared) tea.Cmd {
			return openRetryForm(h, item.Agent)
		}))
	}

	if family, ok := a.triage.findFamily(item, s.listing.agents); ok {
		label := "snooze"
		if a.triage.isSnoozed(family.head.Agent.ID) {
			label = "wake"
		}

		entries = append(entries, submenuEntry{
			label: label,
			keys:  s.keys.Menu.Snooze,
			write: func(m model) (model, tea.Cmd) {
				m.agents.toggleSnooze(&m, m.shared())

				return m, nil
			},
		})
	}

	return entries
}

// editEntries is the entry that opens an editor on the hovered agent's worktree, or closes the
// one already open.
func (a *agentsScreen) editEntries(s panelShared) []submenuEntry {
	if !a.isEditable(s) {
		return nil
	}

	return []submenuEntry{{
		label: a.editDesc(s),
		keys:  s.keys.Menu.Edit,
		write: func(m model) (model, tea.Cmd) {
			cmd := m.agents.pressEdit(&m, m.shared())

			return m, cmd
		},
	}}
}

// workflowEntries are the entries on the hovered workflow.
func workflowEntries(s panelShared, workflow panel.Workflow) []submenuEntry {
	return []submenuEntry{
		openEntry("edit workflow", s.keys.Menu.EditWorkflow, func(h host, s panelShared) tea.Cmd {
			return editWorkflowForm(h, s, workflow)
		}),
		openEntry("adopt", s.keys.Menu.Adopt, func(h host, _ panelShared) tea.Cmd { return openAdopt(h, &workflow) }),
	}
}

var _ screenMenued = (*agentsScreen)(nil)
