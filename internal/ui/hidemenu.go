package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// hideMenu is the agents list's hide menu, hiding the tasks given and gray when there are none.
func (a *agentsScreen) hideMenu(m *model, tasks []panel.Task) submenu {
	return submenu{
		kind:  formHideMenu,
		title: "hide",
		entries: []submenuEntry{
			hideEntry(m, tasks),
			{
				label: a.showHiddenDesc(),
				keys:  m.keys.HideMenu.ShowHidden,
				open:  (*model).toggleHidden,
			},
		},
	}
}

// hideEntry hides or unhides every agent on the tasks, and is gray on a row that is no task.
func hideEntry(m *model, tasks []panel.Task) submenuEntry {
	if len(tasks) == 0 {
		return submenuEntry{
			label: "hide",
			keys:  m.keys.HideMenu.Hide,
			isOff: true,
		}
	}

	label := "hide"
	if isEveryAgentHidden(m.listing.agentsOnTasks(tasks)) {
		label = "unhide"
	}

	return writeEntry(label, m.keys.HideMenu.Hide, func(m *model) tea.Cmd { return hideTasks(m, tasks) })
}

// toggleHidden leaves the menu and shows the hidden agents, or puts them away.
func (m *model) toggleHidden() tea.Cmd {
	m.leaveForm()
	m.reconcileSolo()
	m.agents.toggleHidden(m)

	return nil
}
