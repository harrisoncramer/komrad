package agents

import (
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// hideMenu is the agents list's hide menu, hiding the tasks given and gray when there are none.
func (a *Screen) hideMenu(tasks []panel.Task) menu.Menu {
	return menu.Menu{
		Title: "hide",
		Entries: []menu.Entry{
			a.hideEntry(tasks),
			{
				Label: a.showHiddenDesc(),
				Keys:  a.deps.Keys.HideMenu.ShowHidden,
				Pick: func(in screen.Inputs) screen.Request {
					return screen.Batch{screen.RestoreWindowLayout{}, a.toggleHidden(in)}
				},
			},
		},
	}
}

// hideEntry hides or unhides the tasks, and is gray on a row that is no task.
func (a *Screen) hideEntry(tasks []panel.Task) menu.Entry {
	if len(tasks) == 0 {
		return menu.Entry{
			Label:    "hide",
			Keys:     a.deps.Keys.HideMenu.Hide,
			IsDimmed: true,
		}
	}

	label := "hide"
	if isEveryTaskHidden(tasks) {
		label = "unhide"
	}

	return menu.Holding(label, a.deps.Keys.HideMenu.Hide, func(screen.Inputs) screen.Request { return a.hideTasks(tasks) })
}
