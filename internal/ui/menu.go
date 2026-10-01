package ui

import (
	"slices"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// itemMenued is a screen whose hovered row has a band of its own in the menu.
type itemMenued interface {
	itemMenu(s panelShared) (submenu, bool)
}

// openMenu draws the menu, the hovered row's band over the screen's and the panel's own.
func (m *model) openMenu() tea.Cmd {
	s := m.shared()
	menu := panelMenu(s, m.isScratchOpen())

	if screen, ok := m.active().(screenMenued); ok && !m.isDetailsFocused() {
		if band, ok := screen.screenMenu(s); ok {
			menu = joinMenus(band, menu)
		}
	}

	if screen, ok := m.active().(itemMenued); ok && !m.isDetailsFocused() {
		if item, ok := screen.itemMenu(s); ok {
			menu = joinMenus(item, menu)
		}
	}

	return openSubmenu(m, menu)
}

// panelMenu is the band of the menu that acts on the panel whatever is hovered.
func panelMenu(s panelShared, scratchOpen bool) submenu {
	scratch := "scratch"
	if scratchOpen {
		scratch = "close scratch"
	}

	return submenu{
		kind:  formMenu,
		title: "komrad",
		entries: []submenuEntry{
			{
				label: scratch,
				keys:  s.keys.Menu.Scratch,
				open:  model.openScratch,
			},
			nestedEntry("document types", s.keys.Menu.DocumentTypes, documentTypesMenu(s)),
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

func (a *agentsScreen) itemMenu(s panelShared) (submenu, bool) {
	if a.list().isSelecting(s) {
		agents := chosenAgents(a.selected, a.rows(s))
		if len(agents) == 0 {
			return submenu{}, false
		}

		return withNested(withNested(rowMenu(describeAgents(agents)), "hide", s.keys.Menu.Hide, a.hideMenu(s, nil)), "archive", s.keys.Menu.Archive, agentsArchiveMenu(s, agents, true)), true
	}

	if !a.isBrowsing(s) {
		return submenu{}, false
	}

	item, hasAgent := a.hoveredPaneItem(s)

	if workflow, ok := a.hoveredWorkflow(s); ok {
		menu := rowMenu(workflow.Name)
		if hasAgent {
			menu = agentMenu(s, screenAgents, item, a.editEntries(s)...)
		}
		menu.entries = append(menu.entries, workflowEntries(s, workflow)...)

		return withNested(withNested(menu, "hide", s.keys.Menu.Hide, a.hideMenu(s, nil)), "archive", s.keys.Menu.Archive, workflowArchiveMenu(s, workflow)), true
	}

	if !hasAgent {
		return submenu{}, false
	}

	return withNested(withNested(agentMenu(s, screenAgents, item, a.agentEntries(s, item)...), "hide", s.keys.Menu.Hide, a.hideMenu(s, &item.Agent)), "archive", s.keys.Menu.Archive, agentsArchiveMenu(s, []panel.Agent{item.Agent}, false)), true
}

func (w *schedulesScreen) itemMenu(s panelShared) (submenu, bool) {
	item, ok := w.selected(s)
	if !ok {
		return submenu{}, false
	}

	return withNested(scheduleMenu(s, item.schedule), "archive", s.keys.Menu.Archive, scheduleArchiveMenu(s, item.schedule)), true
}

// screenMenu is the schedules screen's own band, for writing a new schedule.
func (w *schedulesScreen) screenMenu(s panelShared) (submenu, bool) {
	return submenu{
		kind:  formMenu,
		title: "schedules",
		entries: []submenuEntry{
			openEntry("new schedule", s.keys.ScheduleMenu.New, newScheduleForm),
		},
	}, true
}

var _ screenMenued = (*schedulesScreen)(nil)

// scheduleMenu is the band on the hovered schedule.
func scheduleMenu(s panelShared, schedule panel.Schedule) submenu {
	toggle := "pause"
	if !schedule.Enabled {
		toggle = "resume"
	}

	return submenu{
		kind:  formMenu,
		title: schedule.Name,
		entries: []submenuEntry{
			openEntry("edit", s.keys.ScheduleMenu.Edit, func(h host, _ panelShared) tea.Cmd { return editScheduleForm(h, schedule) }),
			writeEntry(toggle, s.keys.ScheduleMenu.Toggle, func(h host) tea.Cmd { return toggleSchedule(h, schedule) }),
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

func (a *archiveScreen) itemMenu(s panelShared) (submenu, bool) {
	target, ok := a.menuTarget(s)
	if !ok {
		return submenu{}, false
	}

	entry := nestedEntry("archive", s.keys.Menu.Archive, archiveMenu(s, target))

	if row, ok := rowAt(a.listRows(s), a.cursor); ok && a.holdsAnAgent(s) && !row.header {
		menu := agentMenu(s, screenArchive, row.item)
		menu.entries = append(menu.entries, entry)

		return menu, true
	}

	menu := rowMenu(target.name(s.listing))
	menu.entries = []submenuEntry{entry}

	return menu, true
}

var (
	_ itemMenued = (*agentsScreen)(nil)
	_ itemMenued = (*archiveScreen)(nil)
	_ itemMenued = (*schedulesScreen)(nil)
	_ itemMenued = (*documentsScreen)(nil)
	_ itemMenued = (*worktreesScreen)(nil)
)

// openScratch leaves the menu for the scratch, or closes the scratch when it is open.
func (m model) openScratch() (tea.Model, tea.Cmd) {
	m.leaveForm()
	cmd := m.toggleScratch()

	return m, cmd
}
