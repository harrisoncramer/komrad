package ui

import (
	"slices"

	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// buildMenu is the hovered row's band over the screen's, false when the screen offers neither.
func (m *model) buildMenu() (menu.Menu, bool) {
	if m.isDetailsFocused() {
		return menu.Menu{}, false
	}

	var item, band menu.Menu
	var hasItem, hasBand bool

	if screen, ok := m.active().(itemMenuBuilder); ok {
		item, hasItem = screen.buildItemMenu(m)
	}

	if screen, ok := m.active().(screenMenuBuilder); ok {
		band, hasBand = screen.buildScreenMenu(m)
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

func (a *agentsScreen) buildItemMenu(m *model) (menu.Menu, bool) {
	if a.list().isSelecting(m) {
		chosen := chosenAgents(a.Selected, a.rows(m))
		if len(chosen) == 0 {
			return menu.Menu{}, false
		}

		work := splitChosen(m.listing, chosen)
		hideable := work.wholeTasks
		if len(work.loneAgents) > 0 {
			hideable = nil
		}
		band := menu.WithNested(rowMenu(describeAgents(chosen)), "hide", m.keys.Menu.Hide, a.hideMenu(m, hideable))

		return menu.WithNested(band, "archive", m.keys.Menu.Archive, chosenArchiveMenu(m, work)), true
	}

	if !a.isBrowsing(m) {
		return menu.Menu{}, false
	}

	row, ok := a.hoveredRow(m)
	if !ok {
		return menu.Menu{}, false
	}
	if task, ok := a.taskRowOf(m, row); ok {
		return a.taskMenu(m, task), true
	}

	item, hasAgent := row.paneItem()

	if workflow, ok := a.hoveredWorkflow(m); ok {
		band := rowMenu(workflow.Name)
		var orchestrator *panel.Agent
		if hasAgent {
			band = agentMenu(m, item, slices.Concat(a.closeEditEntries(m), a.snoozeEntries(m, item))...)
			orchestrator = &item.Agent
		}
		band.Entries = append(band.Entries, workflowEntries(m, workflow, orchestrator)...)

		band = menu.WithNested(band, "hide", m.keys.Menu.Hide, a.hideMenu(m, nil))

		return menu.WithNested(band, "archive", m.keys.Menu.Archive, workflowArchiveMenu(m, workflow)), true
	}

	if !hasAgent {
		return menu.Menu{}, false
	}

	return a.agentRowMenu(m, item), true
}

func (w *schedulesScreen) buildItemMenu(m *model) (menu.Menu, bool) {
	item, ok := w.selected(m)
	if !ok {
		return menu.Menu{}, false
	}

	return menu.WithNested(scheduleMenu(m, item.schedule), "archive", m.keys.Menu.Archive, scheduleArchiveMenu(m, item.schedule)), true
}

// buildScreenMenu builds the schedules screen's own band, for writing a new schedule.
func (w *schedulesScreen) buildScreenMenu(m *model) (menu.Menu, bool) {
	return menu.Menu{
		Title: "schedules",
		Entries: []menu.Entry{
			menu.Leaving("new schedule", m.keys.ScheduleMenu.New, func() tea.Cmd { return newScheduleForm(m) }),
		},
	}, true
}

// scheduleMenu is the band on the hovered schedule.
func scheduleMenu(m *model, schedule panel.Schedule) menu.Menu {
	toggle := "pause"
	if !schedule.Enabled {
		toggle = "resume"
	}

	return menu.Menu{
		Title: schedule.Name,
		Entries: []menu.Entry{
			menu.Leaving("edit", m.keys.ScheduleMenu.Edit, func() tea.Cmd { return editScheduleForm(m, schedule) }),
			menu.Holding(toggle, m.keys.ScheduleMenu.Toggle, func() tea.Cmd { return toggleSchedule(m, schedule) }),
		},
	}
}

// rowMenu is a band titled for a row that has no entries of its own.
func rowMenu(name string) menu.Menu {
	return menu.Menu{
		Title: name,
	}
}
