package ui

import (
	"slices"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// panelChoice is one entry of the panel's own band of the menu.
type panelChoice int

const (
	panelChoiceScratch panelChoice = iota
)

// itemMenued is a screen whose hovered row has a band of its own in the menu.
type itemMenued interface {
	itemMenu(s panelShared) (submenu, bool)
}

// openMenu draws the menu, the hovered row's band over the panel's own.
func (m *model) openMenu() tea.Cmd {
	s := m.shared()
	menu := panelMenu(s, m.isScratchOpen(), m.menuReturn())

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
			{label: scratch, keys: s.keys.Menu.Scratch},
			nestedEntry("document types", s.keys.Menu.DocumentTypes, documentTypesMenu(s, back)),
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			if panelChoice(choice) == panelChoiceScratch {
				m.leaveForm()
				cmd := m.toggleScratch()

				return m, cmd
			}

			return m.closeForm()
		},
	}
}

// joinMenus draws one band over another with a rule between them, under the top band's title.
func joinMenus(top, bottom submenu) submenu {
	offset := len(top.entries) + 1

	return submenu{
		kind:    top.kind,
		title:   top.title,
		crumb:   top.crumb,
		entries: slices.Concat(top.entries, []submenuEntry{dividerEntry}, bottom.entries),
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			if choice < len(top.entries) {
				return top.answer(m, choice)
			}

			return bottom.answer(m, choice-offset)
		},
	}
}

func (a *agentsScreen) itemMenu(s panelShared) (submenu, bool) {
	if a.list().isSelecting(s) {
		agents := chosenAgents(a.selected, a.rows(s))
		if len(agents) == 0 {
			return submenu{}, false
		}

		return withArchive(rowMenu(describeAgents(agents)), s, agentsArchiveMenu(s, agents, true)), true
	}

	if !a.isBrowsing(s) {
		return submenu{}, false
	}

	item, hasAgent := a.hoveredPaneItem(s)

	if workflow, ok := a.hoveredWorkflow(s); ok {
		menu := rowMenu(workflow.Name)
		if hasAgent {
			menu = agentMenu(s, screenAgents, item)
		}

		return withArchive(menu, s, workflowArchiveMenu(s, workflow)), true
	}

	if !hasAgent {
		return submenu{}, false
	}

	return withArchive(agentMenu(s, screenAgents, item), s, agentsArchiveMenu(s, []panel.Agent{item.Agent}, false)), true
}

func (w *schedulesScreen) itemMenu(s panelShared) (submenu, bool) {
	item, ok := w.selected(s)
	if !ok {
		return submenu{}, false
	}

	return withArchive(rowMenu(item.schedule.Name), s, scheduleArchiveMenu(s, item.schedule)), true
}

// rowMenu is a band titled for a row that has no entries of its own.
func rowMenu(name string) submenu {
	return submenu{
		kind:   formMenu,
		title:  name,
		crumb:  name,
		answer: func(m model, _ int) (tea.Model, tea.Cmd) { return m.closeForm() },
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
)
