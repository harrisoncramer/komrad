package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// hideMenu is the agents list's hide menu.
func (a *agentsScreen) hideMenu(s panelShared, agent *panel.Agent) submenu {
	hide := "hide"
	if agent != nil && agent.Hidden {
		hide = "unhide"
	}

	return submenu{
		kind:  formHideMenu,
		title: "hide",
		crumb: "hide",
		entries: []submenuEntry{
			hideEntry(s, hide, agent),
			{
				label: a.showHiddenDesc(),
				keys:  s.keys.HideMenu.ShowHidden,
				open:  model.toggleHidden,
			},
		},
	}
}

// withHide is a band with the hide menu at its foot.
func withHide(menu submenu, s panelShared, hide submenu) submenu {
	at := len(menu.entries)
	menu.entries = append(menu.entries[:at:at], nestedEntry("hide", s.keys.Menu.Hide, hide))

	return menu
}

// hideEntry hides or unhides the agent, and is gray on a row that is not one agent.
func hideEntry(s panelShared, label string, agent *panel.Agent) submenuEntry {
	if agent == nil {
		return submenuEntry{
			label: label,
			keys:  s.keys.HideMenu.Hide,
			isOff: true,
		}
	}

	hidden := *agent

	return writeEntry(label, s.keys.HideMenu.Hide, func(h host) tea.Cmd { return hideAgent(h, hidden) })
}

// toggleHidden leaves the menu and shows the hidden agents, or puts them away.
func (m model) toggleHidden() (tea.Model, tea.Cmd) {
	m.leaveForm()
	m.reconcileSolo()
	m.agents.toggleHidden(&m, m.shared())

	return m, nil
}
