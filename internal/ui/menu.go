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
	case screenAgents, screenArchive, screenSchedules, screenSearch, screenWorktrees:
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
	if !a.holdsAnAgent(s) {
		return submenu{}, false
	}

	row, ok := rowAt(a.listRows(s), a.cursor)
	if !ok || row.header {
		return submenu{}, false
	}

	return agentMenu(s, screenArchive, row.item), true
}

var (
	_ itemMenued = (*agentsScreen)(nil)
	_ itemMenued = (*archiveScreen)(nil)
)
