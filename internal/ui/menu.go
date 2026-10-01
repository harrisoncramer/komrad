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
	menu := panelMenu(s, m.isScratchOpen(), m.menuReturn())

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

// menuPlace is a screen and the target that puts it back as it was.
type menuPlace struct {
	screen screen
	target screenTarget
}

// menuReturn is the screen the menu was opened on and the agent it was showing.
func (m *model) menuReturn() menuPlace {
	switch m.screen {
	case screenDocuments:
		return menuPlace{screen: m.screen, target: screenTarget{agentID: m.documents.agentID, from: m.documents.from}}
	case screenLogs:
		return menuPlace{screen: m.screen, target: screenTarget{agentID: m.logs.agentID, from: m.logs.from}}
	case screenReview:
		return menuPlace{screen: m.screen, target: screenTarget{agentID: m.review.agentID}}
	case screenDocumentTypes:
		return menuPlace{screen: m.documentTypes.from, target: m.documentTypes.returning}
	case screenSearch:
		return menuPlace{screen: m.screen, target: screenTarget{resume: true}}
	case screenAgents, screenArchive, screenSchedules, screenWorktrees:
	}

	return menuPlace{screen: m.screen, target: screenTarget{agentID: m.hoveredAgentID()}}
}

// panelMenu is the band of the menu that acts on the panel whatever is hovered.
func panelMenu(s panelShared, scratchOpen bool, back menuPlace) submenu {
	scratch := "scratch"
	if scratchOpen {
		scratch = "close scratch"
	}

	return submenu{
		kind:  formMenu,
		title: "komrad",
		crumb: "komrad",
		entries: []submenuEntry{
			{
				label: scratch,
				keys:  s.keys.Menu.Scratch,
				open:  model.openScratch,
			},
			nestedEntry("document types", s.keys.Menu.DocumentTypes, documentTypesMenu(s, back)),
		},
	}
}

// joinMenus draws one band over another with a rule between them, under the top band's title.
func joinMenus(top, bottom submenu) submenu {
	return submenu{
		kind:    top.kind,
		title:   top.title,
		crumb:   top.crumb,
		entries: slices.Concat(top.entries, []submenuEntry{dividerEntry}, bottom.entries),
	}
}

func (a *agentsScreen) itemMenu(s panelShared) (submenu, bool) {
	if a.list().isSelecting(s) {
		agents := chosenAgents(a.selected, a.rows(s))
		if len(agents) == 0 {
			return submenu{}, false
		}

		return withArchive(withHide(rowMenu(describeAgents(agents)), s, a.hideMenu(s, nil)), s, agentsArchiveMenu(s, agents, true)), true
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

		return withArchive(withHide(menu, s, a.hideMenu(s, nil)), s, workflowArchiveMenu(s, workflow)), true
	}

	if !hasAgent {
		return submenu{}, false
	}

	return withArchive(withHide(agentMenu(s, screenAgents, item, a.agentEntries(s, item)...), s, a.hideMenu(s, &item.Agent)), s, agentsArchiveMenu(s, []panel.Agent{item.Agent}, false)), true
}

func (w *schedulesScreen) itemMenu(s panelShared) (submenu, bool) {
	item, ok := w.selected(s)
	if !ok {
		return submenu{}, false
	}

	return withArchive(scheduleMenu(s, item.schedule), s, scheduleArchiveMenu(s, item.schedule)), true
}

// screenMenu is the schedules screen's own band, for writing a new schedule.
func (w *schedulesScreen) screenMenu(s panelShared) (submenu, bool) {
	return submenu{
		kind:  formMenu,
		title: "schedules",
		crumb: "schedules",
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
		crumb: schedule.Name,
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
		crumb: name,
	}
}

// withArchive is a band with the archive menu at its foot.
func withArchive(menu submenu, s panelShared, archive submenu) submenu {
	at := len(menu.entries)
	menu.entries = append(menu.entries[:at:at], nestedEntry("archive", s.keys.Menu.Archive, archive))

	return menu
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
