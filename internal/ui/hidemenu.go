package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// hideMenu is the agents list's hide menu, hiding the agents given and gray when there are none.
func (a *agentsScreen) hideMenu(m *model, agents []panel.Agent, subject string) submenu {
	return submenu{
		kind:  formHideMenu,
		title: "hide",
		entries: []submenuEntry{
			hideEntry(m, agents, subject),
			{
				label: a.showHiddenDesc(),
				keys:  m.keys.HideMenu.ShowHidden,
				open:  (*model).toggleHidden,
			},
		},
	}
}

// hideEntry hides or unhides the agents, and is gray when there are none.
func hideEntry(m *model, agents []panel.Agent, subject string) submenuEntry {
	if len(agents) == 0 {
		return submenuEntry{
			label: "hide",
			keys:  m.keys.HideMenu.Hide,
			isOff: true,
		}
	}

	label := "hide"
	if isEveryAgentHidden(agents) {
		label = "unhide"
	}

	return writeEntry(label, m.keys.HideMenu.Hide, func(m *model) tea.Cmd { return hideAgents(m, agents, subject) })
}

// toggleHidden leaves the menu and shows the hidden agents, or puts them away.
func (m *model) toggleHidden() tea.Cmd {
	m.leaveForm()
	m.reconcileSolo()
	m.agents.toggleHidden(m)

	return nil
}
