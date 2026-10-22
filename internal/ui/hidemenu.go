package ui

import (
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// hideMenu is the agents list's hide menu, hiding the tasks given and gray when there are none.
func (a *agentsScreen) hideMenu(m *model, tasks []panel.Task) menu.Menu {
	return menu.Menu{
		Title: "hide",
		Entries: []menu.Entry{
			a.hideEntry(tasks),
			{
				Label: a.showHiddenDesc(),
				Keys:  a.deps.keys.HideMenu.ShowHidden,
				Pick:  m.toggleHidden,
			},
		},
	}
}

// hideEntry hides or unhides the tasks, and is gray on a row that is no task.
func (a *agentsScreen) hideEntry(tasks []panel.Task) menu.Entry {
	if len(tasks) == 0 {
		return menu.Entry{
			Label: "hide",
			Keys:  a.deps.keys.HideMenu.Hide,
			Off:   true,
		}
	}

	label := "hide"
	if isEveryTaskHidden(tasks) {
		label = "unhide"
	}

	return menu.Holding(label, a.deps.keys.HideMenu.Hide, func() screen.Request { return a.hideTasks(tasks) })
}

// toggleHidden shows the hidden tasks, or puts them away.
func (m *model) toggleHidden() screen.Request {
	return screen.Batch{screen.Reconcile{}, m.agents.toggleHidden(m.inputs())}
}
