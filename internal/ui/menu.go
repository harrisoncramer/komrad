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
		item, hasItem = screen.buildItemMenu(m, m.inputs())
	}

	if screen, ok := m.active().(screenMenuBuilder); ok {
		band, hasBand = screen.buildScreenMenu(m, m.inputs())
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

func (a *agentsScreen) buildItemMenu(m *model, in screen.Inputs) (menu.Menu, bool) {
	if a.list().isSelecting() {
		chosen := chosenAgents(a.Selected, a.rows(in))
		if len(chosen) == 0 {
			return menu.Menu{}, false
		}

		work := splitChosen(in.Listing, chosen)
		hideable := work.wholeTasks
		if len(work.loneAgents) > 0 {
			hideable = nil
		}
		band := menu.WithNested(rowMenu(describeAgents(chosen)), "hide", a.deps.keys.Menu.Hide, a.hideMenu(m, hideable))

		return menu.WithNested(band, "archive", a.deps.keys.Menu.Archive, a.chosenArchiveMenu(m, work)), true
	}

	if !a.isBrowsing() {
		return menu.Menu{}, false
	}

	row, ok := a.hoveredRow(in)
	if !ok {
		return menu.Menu{}, false
	}
	if task, ok := a.taskRowOf(m, row); ok {
		return a.taskMenu(m, in, task), true
	}

	item, hasAgent := row.paneItem()

	if workflow, ok := a.hoveredWorkflow(in); ok {
		band := rowMenu(workflow.Name)
		var orchestrator *panel.Agent
		if hasAgent {
			band = agentMenu(m, a.deps.keys, item, slices.Concat(a.closeEditEntries(m), a.snoozeEntries(m, in, item))...)
			orchestrator = &item.Agent
		}
		band.Entries = append(band.Entries, a.workflowEntries(m, workflow, orchestrator)...)

		band = menu.WithNested(band, "hide", a.deps.keys.Menu.Hide, a.hideMenu(m, nil))

		return menu.WithNested(band, "archive", a.deps.keys.Menu.Archive, a.workflowArchiveMenu(m, workflow)), true
	}

	if !hasAgent {
		return menu.Menu{}, false
	}

	return a.agentRowMenu(m, in, item), true
}

func (w *schedulesScreen) buildItemMenu(m *model, in screen.Inputs) (menu.Menu, bool) {
	item, ok := w.selected(in)
	if !ok {
		return menu.Menu{}, false
	}

	return menu.WithNested(w.scheduleMenu(item.schedule), "archive", w.deps.keys.Menu.Archive, w.scheduleArchiveMenu(item.schedule)), true
}

// buildScreenMenu builds the schedules screen's own band, for writing a new schedule.
func (w *schedulesScreen) buildScreenMenu(m *model, in screen.Inputs) (menu.Menu, bool) {
	return menu.Menu{
		Title: "schedules",
		Entries: []menu.Entry{
			menu.Leaving("new schedule", w.deps.keys.ScheduleMenu.New, func() screen.Request { return w.newScheduleForm(m) }),
		},
	}, true
}

// scheduleMenu is the band on the hovered schedule.
func (w *schedulesScreen) scheduleMenu(schedule panel.Schedule) menu.Menu {
	toggle := "pause"
	if !schedule.Enabled {
		toggle = "resume"
	}

	return menu.Menu{
		Title: schedule.Name,
		Entries: []menu.Entry{
			menu.Leaving("edit", w.deps.keys.ScheduleMenu.Edit, func() screen.Request { return w.editScheduleForm(schedule) }),
			menu.Holding(toggle, w.deps.keys.ScheduleMenu.Toggle, func() screen.Request { return w.toggleSchedule(schedule) }),
		},
	}
}

// rowMenu is a band titled for a row that has no entries of its own.
func rowMenu(name string) menu.Menu {
	return menu.Menu{
		Title: name,
	}
}
