package ui

import (
	"strings"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// documentsChoice is one entry of the documents menu.
type documentsChoice int

const (
	documentsChoiceView documentsChoice = iota
	documentsChoiceNew
)

// documentsMenu is the ways into an agent's documents, opened from its menu.
func documentsMenu(s panelShared, from screen, item panel.AgentRow) submenu {
	agentID := item.Agent.ID

	return submenu{
		kind:  formDocumentsMenu,
		title: "documents",
		crumb: "documents",
		entries: []submenuEntry{
			{
				label: "view documents",
				keys:  s.keys.DocumentsMenu.View,
				isOff: item.Documents == 0,
			},
			{label: "new document", keys: s.keys.DocumentsMenu.New},
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			return m.runDocumentsChoice(from, agentID, documentsChoice(choice))
		},
	}
}

// runDocumentsChoice does what the entry asked for, or says why the agent is not one to ask it of.
func (m model) runDocumentsChoice(from screen, agentID string, choice documentsChoice) (tea.Model, tea.Cmd) {
	switch choice {
	case documentsChoiceView:
		return m.enterSubpage(from, agentID, subpageDocuments)
	case documentsChoiceNew:
		if _, ok := m.listing.anyAgent(agentID); !ok {
			return m.refuseSubpage("Could not write a document", errAgentGone)
		}

		m.leaveForm()
		cmd := newDocumentForm(&m, m.shared(), agentID)

		return m, cmd
	}

	return m.closeForm()
}

// documentTypesChoice is one entry of the document types menu.
type documentTypesChoice int

const (
	documentTypesChoiceList documentTypesChoice = iota
	documentTypesChoiceNew
)

// documentTypesMenu is the ways into the document types, opened from the panel's own entries.
func documentTypesMenu(s panelShared, back menuPlace) submenu {
	return submenu{
		kind:  formDocumentTypesMenu,
		title: "document types",
		crumb: "document types",
		entries: []submenuEntry{
			{label: "list types", keys: s.keys.DocumentTypesMenu.List},
			{label: "new type", keys: s.keys.DocumentTypesMenu.New},
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			switch documentTypesChoice(choice) {
			case documentTypesChoiceList:
				m.leaveForm()
				cmd := m.enter(screenDocumentTypes, screenTarget{from: back.screen})
				m.documentTypes.returning = back.target

				return m, cmd
			case documentTypesChoiceNew:
				m.leaveForm()
				cmd := newTypeForm(&m)

				return m, cmd
			}

			return m.closeForm()
		},
	}
}

// documentEntry is one entry of the menu on a document or a document type.
type documentEntry struct {
	entry submenuEntry
	run   func(d *documentsScreen, h host, s panelShared) tea.Cmd
}

func (d *documentsScreen) itemMenu(s panelShared) (submenu, bool) {
	if d.isDiffFocused(s) {
		return submenu{}, false
	}

	return documentMenu(s, d), true
}

// documentMenu is the actions on the hovered document or type, and on the shelf it is listed on.
func documentMenu(s panelShared, d *documentsScreen) submenu {
	item, hovered := d.hovered(s)
	title := strings.Join(d.crumbRoot(s), " ")
	if hovered {
		title = item.name
	}

	entries := []documentEntry{
		{
			entry: submenuEntry{label: d.editLabel(), keys: s.keys.DocumentMenu.Edit, isOff: !hovered && d.editing == nil},
			run:   (*documentsScreen).pressEdit,
		},
		{
			entry: submenuEntry{label: "rename", keys: s.keys.DocumentMenu.Rename, isOff: !hovered || d.editing != nil},
			run:   (*documentsScreen).openRename,
		},
		{
			entry: submenuEntry{label: d.newDesc(), keys: s.keys.DocumentMenu.New, isOff: d.editing != nil},
			run:   (*documentsScreen).pressNew,
		},
	}
	if d.shelf == shelfDocumentTypes {
		entries = append(entries, documentEntry{
			entry: submenuEntry{label: "delete", keys: s.keys.DocumentMenu.Delete, isOff: !hovered || d.editing != nil},
			run:   (*documentsScreen).confirmDeleteType,
		})
	}

	on := d.shelf.screen()

	return submenu{
		kind:    formMenu,
		title:   title,
		crumb:   title,
		entries: xslice.Map(entries, func(e documentEntry) submenuEntry { return e.entry }),
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			m.leaveForm()
			cmd := entries[choice].run(m.documentsOn(on), &m, m.shared())

			return m, cmd
		},
	}
}
