package ui

import (
	"slices"

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
	if !a.holdsSubpages(s) {
		return submenu{}, false
	}

	item, ok := a.hoveredPaneItem(s)
	if !ok {
		return submenu{}, false
	}

	return agentMenu(s, screenAgents, item), true
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

	name := target.name(s.listing)

	return submenu{
		kind:    formMenu,
		title:   name,
		crumb:   name,
		entries: []submenuEntry{entry},
		answer:  func(m model, _ int) (tea.Model, tea.Cmd) { return m.closeForm() },
	}, true
}

var (
	_ itemMenued = (*agentsScreen)(nil)
	_ itemMenued = (*archiveScreen)(nil)
)
