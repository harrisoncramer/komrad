package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// panelChoice is one entry of the panel's own band of the menu.
type panelChoice int

const (
	panelChoiceScratch panelChoice = iota
	panelChoiceDocumentTypes
)

// itemMenued is a screen whose hovered row has a band of its own in the menu.
type itemMenued interface {
	itemMenu(s panelShared) (submenu, bool)
}

// openMenu draws the menu, the hovered row's band over the panel's own.
func (m *model) openMenu() tea.Cmd {
	s := m.shared()
	_, typesHere := m.documentTypesTarget()
	menu := panelMenu(s, m.isScratchOpen(), typesHere)

	if screen, ok := m.active().(itemMenued); ok && !m.isDetailsFocused() {
		if item, ok := screen.itemMenu(s); ok {
			menu = joinMenus(item, menu)
		}
	}

	return openSubmenu(m, menu)
}

// panelMenu is the band of the menu that acts on the panel whatever is hovered, the document types
// gray on a screen they do not open from.
func panelMenu(s panelShared, scratchOpen, typesHere bool) submenu {
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
			{label: "document types", keys: s.keys.Menu.DocumentTypes, isOff: !typesHere},
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			switch panelChoice(choice) {
			case panelChoiceScratch:
				m.leaveForm()
				cmd := m.toggleScratch()

				return m, cmd
			case panelChoiceDocumentTypes:
				target, ok := m.documentTypesTarget()
				if !ok {
					return m.refuseSubpage("Could not open the document types", errDocumentTypesHere)
				}

				m.leaveForm()

				return m, m.enter(screenDocumentTypes, target)
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
