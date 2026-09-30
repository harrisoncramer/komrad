package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// documentsChoice is one entry of the documents menu.
type documentsChoice int

const (
	documentsChoiceView documentsChoice = iota
	documentsChoiceNew
	documentsChoiceNewType
	documentsChoiceTypes
)

// documentsMenu is the ways into an agent's documents and the types they are written as, opened
// from its menu.
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
			{label: "new type", keys: s.keys.DocumentsMenu.NewType},
			{label: "list types", keys: s.keys.DocumentsMenu.Types},
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
	case documentsChoiceNewType:
		m.leaveForm()
		cmd := newTypeForm(&m)

		return m, cmd
	case documentsChoiceTypes:
		m.leaveForm()
		cmd := m.enter(screenDocumentTypes, screenTarget{agentID: agentID, from: from})

		return m, cmd
	}

	return m.closeForm()
}
