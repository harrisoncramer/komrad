package ui

import (
	"strings"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
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
				open: func(m model) (tea.Model, tea.Cmd) {
					return m.enterSubpage(from, agentID, subpageDocuments)
				},
			},
			{
				label: "new document",
				keys:  s.keys.DocumentsMenu.New,
				open: func(m model) (tea.Model, tea.Cmd) {
					return m.openNewDocument(agentID)
				},
			},
		},
	}
}

// openNewDocument leaves the menu for the form that writes an agent a document, or says why the
// agent is not one to write for.
func (m model) openNewDocument(agentID string) (tea.Model, tea.Cmd) {
	if _, ok := m.listing.anyAgent(agentID); !ok {
		return m.refuseSubpage("Could not write a document", errAgentGone)
	}

	m.leaveForm()
	cmd := newDocumentForm(&m, m.shared(), agentID)

	return m, cmd
}

// documentTypesMenu is the ways into the document types, opened from the panel's own entries.
func documentTypesMenu(s panelShared, back menuPlace) submenu {
	return submenu{
		kind:  formDocumentTypesMenu,
		title: "document types",
		crumb: "document types",
		entries: []submenuEntry{
			{
				label: "list types",
				keys:  s.keys.DocumentTypesMenu.List,
				open: func(m model) (tea.Model, tea.Cmd) {
					m.leaveForm()
					cmd := m.enter(screenDocumentTypes, screenTarget{from: back.screen})
					m.documentTypes.returning = back.target

					return m, cmd
				},
			},
			{
				label: "new type",
				keys:  s.keys.DocumentTypesMenu.New,
				open: func(m model) (tea.Model, tea.Cmd) {
					m.leaveForm()
					cmd := newTypeForm(&m)

					return m, cmd
				},
			},
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
			entry: submenuEntry{
				label: d.editLabel(),
				keys:  s.keys.DocumentMenu.Edit,
				isOff: !hovered && d.editing == nil,
			},
			run: (*documentsScreen).pressEdit,
		},
		{
			entry: submenuEntry{
				label: "rename",
				keys:  s.keys.DocumentMenu.Rename,
				isOff: !hovered || d.editing != nil,
			},
			run: (*documentsScreen).openRename,
		},
		{
			entry: submenuEntry{
				label: d.newDesc(),
				keys:  s.keys.DocumentMenu.New,
				isOff: d.editing != nil,
			},
			run: (*documentsScreen).pressNew,
		},
	}
	if d.shelf == shelfDocumentTypes {
		entries = append(entries, documentEntry{
			entry: submenuEntry{
				label: "delete",
				keys:  s.keys.DocumentMenu.Delete,
				isOff: !hovered || d.editing != nil,
			},
			run: (*documentsScreen).confirmDeleteType,
		})
	}

	on := d.shelf.screen()

	return submenu{
		kind:  formMenu,
		title: title,
		crumb: title,
		entries: xslice.Map(entries, func(e documentEntry) submenuEntry {
			entry, run := e.entry, e.run
			entry.open = func(m model) (tea.Model, tea.Cmd) {
				m.leaveForm()
				cmd := run(m.documentsOn(on), &m, m.shared())

				return m, cmd
			}

			return entry
		}),
	}
}
