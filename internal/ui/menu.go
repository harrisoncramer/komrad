package ui

import (
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
)

// buildMenu is the hovered row's band over the screen's, false when the screen offers neither.
func (m *model) buildMenu() (menu.Menu, bool) {
	if m.isDetailsFocused(m.inputs()) {
		return menu.Menu{}, false
	}

	var item, band menu.Menu
	var hasItem, hasBand bool

	if screen, ok := m.active().(itemMenuBuilder); ok {
		item, hasItem = screen.BuildItemMenu(m.inputs(), m)
	}

	if screen, ok := m.active().(screenMenuBuilder); ok {
		band, hasBand = screen.BuildScreenMenu(m.inputs(), m)
	}

	switch {
	case hasItem && hasBand:
		return menu.Join(item, band), true
	case hasItem:
		return item, true
	default:
		return band, hasBand
	}
}

// Git is the git actions on a task's checkout and pull request.
func (m *model) Git(task panel.Task) menu.Menu {
	return m.git.Git(m.listing, task)
}

// PullRequestGit is the git menu holding only a task's push and its forge menu.
func (m *model) PullRequestGit(task panel.Task) menu.Menu {
	return m.git.PullRequestGit(m.listing, task)
}
