package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// hideChoice is one entry of the hide menu.
type hideChoice int

const (
	hideChoiceHide hideChoice = iota
	hideChoiceShowHidden
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
			{label: hide, keys: s.keys.HideMenu.Hide, isOff: agent == nil},
			{label: a.showHiddenDesc(), keys: s.keys.HideMenu.ShowHidden},
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			if hideChoice(choice) == hideChoiceHide && agent != nil {
				return m.submitMenu(func(h host) tea.Cmd { return hideAgent(h, *agent) })
			}

			m.leaveForm()
			m.agents.toggleHidden(&m, m.shared())

			return m, nil
		},
	}
}

// withHide is a band with the hide menu at its foot.
func withHide(menu submenu, s panelShared, hide submenu) submenu {
	at := len(menu.entries)
	menu.entries = append(menu.entries[:at:at], nestedEntry("hide", s.keys.Menu.Hide, hide))

	return menu
}
