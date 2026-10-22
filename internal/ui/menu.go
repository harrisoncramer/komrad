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
		item, hasItem = screen.buildItemMenu(m.inputs(), m)
	}

	if screen, ok := m.active().(screenMenuBuilder); ok {
		band, hasBand = screen.buildScreenMenu(m.inputs(), m)
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

func (a *agentsScreen) buildItemMenu(in screen.Inputs, nest menu.Nester) (menu.Menu, bool) {
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

func (w *schedulesScreen) buildItemMenu(in screen.Inputs, _ menu.Nester) (menu.Menu, bool) {
	item, ok := w.selected(in)
	if !ok {
		return menu.Menu{}, false
	}

	return menu.WithNested(w.scheduleMenu(item.schedule), "archive", w.deps.keys.Menu.Archive, w.scheduleArchiveMenu(item.schedule)), true
}

// buildScreenMenu builds the schedules screen's own band, for writing a new schedule.
func (w *schedulesScreen) buildScreenMenu(_ screen.Inputs, _ menu.Nester) (menu.Menu, bool) {
	return menu.Menu{
		Title: "schedules",
		Entries: []menu.Entry{
			menu.Leaving("new schedule", w.deps.keys.ScheduleMenu.New, w.newScheduleForm),
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
			menu.Leaving("edit", w.deps.keys.ScheduleMenu.Edit, func(screen.Inputs) screen.Request { return w.editScheduleForm(schedule) }),
			menu.Holding(toggle, w.deps.keys.ScheduleMenu.Toggle, func(screen.Inputs) screen.Request { return w.toggleSchedule(schedule) }),
		},
	}
}

// rowMenu is a band titled for a row that has no entries of its own.
func rowMenu(name string) menu.Menu {
	return menu.Menu{
		Title: name,
	}
}
