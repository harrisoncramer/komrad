package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// hideMenu is the agents list's hide menu.
func (a *agentsScreen) hideMenu(m *model, agent *panel.Agent) submenu {
	hide := "hide"
	if agent != nil && agent.Hidden {
		hide = "unhide"
	}

	return submenu{
		kind:  formHideMenu,
		title: "hide",
		entries: []submenuEntry{
			hideEntry(m, hide, agent),
			{
				label: a.showHiddenDesc(),
				keys:  m.keys.HideMenu.ShowHidden,
				open:  (*model).toggleHidden,
			},
		},
	}
}

// hideEntry hides or unhides the agent, and is gray on a row that is not one agent.
func hideEntry(m *model, label string, agent *panel.Agent) submenuEntry {
	if agent == nil {
		return submenuEntry{
			label: label,
			keys:  m.keys.HideMenu.Hide,
			isOff: true,
		}
	}

	hidden := *agent

	return writeEntry(label, m.keys.HideMenu.Hide, func(m *model) tea.Cmd { return hideAgent(m, hidden) })
}

// toggleHidden leaves the menu and shows the hidden agents, or puts them away.
func (m *model) toggleHidden() tea.Cmd {
	m.leaveForm()
	m.reconcileSolo()
	m.agents.toggleHidden(m)

	return nil
}
