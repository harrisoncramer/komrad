package ui

import (
	"slices"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// openMenu draws the menu, the hovered row's band over the screen's and the panel's own.
func (m *model) openMenu() tea.Cmd {
	menu := panelMenu(m)

	if screen, ok := m.active().(screenMenuBuilder); ok && !m.isDetailsFocused() {
		if band, ok := screen.buildScreenMenu(m); ok {
			menu = joinMenus(band, menu)
		}
	}

	if screen, ok := m.active().(itemMenuBuilder); ok && !m.isDetailsFocused() {
		if item, ok := screen.buildItemMenu(m); ok {
			menu = joinMenus(item, menu)
		}
	}

	return openSubmenu(m, menu)
}

// panelMenu is the band of the menu that acts on the panel whatever is hovered.
func panelMenu(m *model) submenu {
	scratch := "scratch"
	if m.isScratchOpen() {
		scratch = "close scratch"
	}

	return submenu{
		kind:  formMenu,
		title: "komrad",
		entries: []submenuEntry{
			{
				label: scratch,
				keys:  m.keys.Menu.Scratch,
				open:  (*model).openScratch,
			},
			nestedEntry("document types", m.keys.Menu.DocumentTypes, documentTypesMenu(m)),
		},
	}
}

// joinMenus draws one band over another with a rule between them, under the top band's title.
func joinMenus(top, bottom submenu) submenu {
	return submenu{
		kind:    top.kind,
		title:   top.title,
		entries: slices.Concat(top.entries, []submenuEntry{dividerEntry}, bottom.entries),
	}
}

func (a *agentsScreen) buildItemMenu(m *model) (submenu, bool) {
	if a.list().isSelecting(m) {
		agents := chosenAgents(a.selected, a.rows(m))
		if len(agents) == 0 {
			return submenu{}, false
		}

		menu := withNested(rowMenu(describeAgents(agents)), "hide", m.keys.Menu.Hide, a.hideMenu(m, nil))

		return withNested(menu, "archive", m.keys.Menu.Archive, agentsArchiveMenu(m, agents, true)), true
	}

	if !a.isBrowsing(m) {
		return submenu{}, false
	}

	item, hasAgent := a.hoveredPaneItem(m)

	if workflow, ok := a.hoveredWorkflow(m); ok {
		menu := rowMenu(workflow.Name)
		if hasAgent {
			menu = agentMenu(m, screenAgents, item, a.editEntries(m)...)
		}
		menu.entries = append(menu.entries, workflowEntries(m, workflow)...)

		menu = withNested(menu, "hide", m.keys.Menu.Hide, a.hideMenu(m, nil))

		return withNested(menu, "archive", m.keys.Menu.Archive, workflowArchiveMenu(m, workflow)), true
	}

	if !hasAgent {
		return submenu{}, false
	}

	menu := withNested(agentMenu(m, screenAgents, item, a.agentEntries(m, item)...), "hide", m.keys.Menu.Hide, a.hideMenu(m, &item.Agent))

	return withNested(menu, "archive", m.keys.Menu.Archive, agentsArchiveMenu(m, []panel.Agent{item.Agent}, false)), true
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

func (a *archiveScreen) buildItemMenu(m *model) (submenu, bool) {
	target, ok := a.menuTarget(m)
	if !ok {
		return submenu{}, false
	}

	archive := archiveMenu(m, target)

	if row, ok := rowAt(a.listRows(m), a.cursor); ok && a.holdsAnAgent(m) && !row.header {
		return withNested(agentMenu(m, screenArchive, row.item), "archive", m.keys.Menu.Archive, archive), true
	}

	return withNested(rowMenu(target.name(m.listing)), "archive", m.keys.Menu.Archive, archive), true
}

// openScratch leaves the menu for the scratch, or closes the scratch when it is open.
func (m *model) openScratch() tea.Cmd {
	m.leaveForm()
	return m.toggleScratch()
}
