package ui

import (
	"slices"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// buildMenu is the hovered row's band over the screen's, false when the screen offers neither.
func (m *model) buildMenu() (submenu, bool) {
	if m.isDetailsFocused() {
		return submenu{}, false
	}

	var item, band submenu
	var hasItem, hasBand bool

	if screen, ok := m.active().(itemMenuBuilder); ok {
		item, hasItem = screen.buildItemMenu(m)
	}

	if screen, ok := m.active().(screenMenuBuilder); ok {
		band, hasBand = screen.buildScreenMenu(m)
	}

	switch {
	case hasItem && hasBand:
		return joinMenus(item, band), true
	case hasItem:
		return item, true
	default:
		return band, hasBand
	}
}

// joinMenus renders one band over another with a rule between them, under the top band's title.
func joinMenus(top, bottom submenu) submenu {
	return submenu{
		kind:    top.kind,
		title:   top.title,
		entries: slices.Concat(top.entries, []submenuEntry{dividerEntry}, bottom.entries),
	}
}

func (a *agentsScreen) buildItemMenu(m *model) (submenu, bool) {
	if a.list().isSelecting(m) {
		chosen := chosenAgents(a.Selected, a.rows(m))
		if len(chosen) == 0 {
			return submenu{}, false
		}

		work := splitChosen(m.listing, chosen)
		hideable := work.wholeTasks
		if len(work.loneAgents) > 0 {
			hideable = nil
		}
		menu := withNested(rowMenu(describeAgents(chosen)), "hide", m.keys.Menu.Hide, a.hideMenu(m, hideable))

		return withNested(menu, "archive", m.keys.Menu.Archive, chosenArchiveMenu(m, work)), true
	}

	if !a.isBrowsing(m) {
		return submenu{}, false
	}

	row, ok := a.hoveredRow(m)
	if !ok {
		return submenu{}, false
	}
	if task, ok := a.taskRowOf(m, row); ok {
		return a.taskMenu(m, task), true
	}

	item, hasAgent := row.paneItem()

	if workflow, ok := a.hoveredWorkflow(m); ok {
		menu := rowMenu(workflow.Name)
		var orchestrator *panel.Agent
		if hasAgent {
			menu = agentMenu(m, item, slices.Concat(a.closeEditEntries(m), a.snoozeEntries(m, item))...)
			orchestrator = &item.Agent
		}
		menu.entries = append(menu.entries, workflowEntries(m, workflow, orchestrator)...)

		menu = withNested(menu, "hide", m.keys.Menu.Hide, a.hideMenu(m, nil))

		return withNested(menu, "archive", m.keys.Menu.Archive, workflowArchiveMenu(m, workflow)), true
	}

	if !hasAgent {
		return submenu{}, false
	}

	return a.agentRowMenu(m, item), true
}

func (w *schedulesScreen) buildItemMenu(m *model) (submenu, bool) {
	item, ok := w.selected(m)
	if !ok {
		return submenu{}, false
	}

	return withNested(scheduleMenu(m, item.schedule), "archive", m.keys.Menu.Archive, scheduleArchiveMenu(m, item.schedule)), true
}

// buildScreenMenu builds the schedules screen's own band, for writing a new schedule.
func (w *schedulesScreen) buildScreenMenu(m *model) (submenu, bool) {
	return submenu{
		kind:  formMenu,
		title: "schedules",
		entries: []submenuEntry{
			openEntry("new schedule", m.keys.ScheduleMenu.New, newScheduleForm),
		},
	}, true
}

// scheduleMenu is the band on the hovered schedule.
func scheduleMenu(m *model, schedule panel.Schedule) submenu {
	toggle := "pause"
	if !schedule.Enabled {
		toggle = "resume"
	}

	return submenu{
		kind:  formMenu,
		title: schedule.Name,
		entries: []submenuEntry{
			openEntry("edit", m.keys.ScheduleMenu.Edit, func(m *model) tea.Cmd { return editScheduleForm(m, schedule) }),
			writeEntry(toggle, m.keys.ScheduleMenu.Toggle, func(m *model) tea.Cmd { return toggleSchedule(m, schedule) }),
		},
	}
}

// rowMenu is a band titled for a row that has no entries of its own.
func rowMenu(name string) submenu {
	return submenu{
		kind:  formMenu,
		title: name,
	}
}
