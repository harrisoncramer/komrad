package ui

import (
	"slices"

	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
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

func (a *agentsScreen) BuildItemMenu(in screen.Inputs, nest menu.Nester) (menu.Menu, bool) {
	if a.list().IsSelecting() {
		chosen := chosenAgents(a.Selected, a.rows(in))
		if len(chosen) == 0 {
			return menu.Menu{}, false
		}

		work := splitChosen(in.Listing, chosen)
		hideable := work.wholeTasks
		if len(work.loneAgents) > 0 {
			hideable = nil
		}
		band := menu.WithNested(rowMenu(describeAgents(chosen)), "hide", a.deps.keys.Menu.Hide, a.hideMenu(hideable))

		return menu.WithNested(band, "archive", a.deps.keys.Menu.Archive, a.chosenArchiveMenu(work)), true
	}

	if !a.isBrowsing() {
		return menu.Menu{}, false
	}

	row, ok := a.hoveredRow(in)
	if !ok {
		return menu.Menu{}, false
	}
	if task, ok := a.taskRowOf(in, row); ok {
		return a.taskMenu(in, nest, task), true
	}

	item, hasAgent := row.paneItem()

	if workflow, ok := a.hoveredWorkflow(in); ok {
		band := rowMenu(workflow.Name)
		var orchestrator *panel.Agent
		if hasAgent {
			band = agentMenu(agentMenuParams{
				in:       in,
				nest:     nest,
				bindings: a.deps.keys,
				item:     item,
				own:      slices.Concat(a.closeEditEntries(), a.snoozeEntries(in, item)),
			})
			orchestrator = &item.Agent
		}
		band.Entries = append(band.Entries, a.workflowEntries(workflow, orchestrator)...)

		band = menu.WithNested(band, "hide", a.deps.keys.Menu.Hide, a.hideMenu(nil))

		return menu.WithNested(band, "archive", a.deps.keys.Menu.Archive, a.workflowArchiveMenu(workflow)), true
	}

	if !hasAgent {
		return menu.Menu{}, false
	}

	return a.agentRowMenu(in, item), true
}

// Git is the git actions on a task's checkout and pull request.
func (m *model) Git(task panel.Task) menu.Menu {
	return m.git.Git(m.listing, task)
}

// PullRequestGit is the git menu holding only a task's pull request entries.
func (m *model) PullRequestGit(task panel.Task) menu.Menu {
	return m.git.PullRequestGit(m.listing, task)
}

// rowMenu is a band titled for a row that has no entries of its own.
func rowMenu(name string) menu.Menu {
	return menu.Menu{
		Title: name,
	}
}
