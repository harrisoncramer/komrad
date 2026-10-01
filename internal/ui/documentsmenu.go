package ui

import (
	"fmt"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// documentsMenu is the ways into an agent's documents, opened from its menu.
func documentsMenu(m *model, from screen, item panel.AgentRow) submenu {
	agentID := item.Agent.ID

	return submenu{
		kind:  formDocumentsMenu,
		title: "documents",
		entries: []submenuEntry{
			{
				label: "view documents",
				keys:  m.keys.DocumentsMenu.View,
				isOff: item.Documents == 0,
				open: func(m *model) tea.Cmd {
					return m.enterSubpage(from, agentID, subpageDocuments)
				},
			},
			{
				label: "new document",
				keys:  m.keys.DocumentsMenu.New,
				open: func(m *model) tea.Cmd {
					return m.openNewDocument(agentID)
				},
			},
		},
	}
}

// openNewDocument leaves the menu for the form that writes an agent a document, or says why the
// agent is not one to write for.
func (m *model) openNewDocument(agentID string) tea.Cmd {
	if _, ok := m.listing.anyAgent(agentID); !ok {
		return m.refuseSubpage("Could not write a document", errAgentGone)
	}

	m.leaveForm()

	return newDocumentForm(m, agentID)
}

// documentTypesMenu is every document type, the context of the one under the cursor drawn beside
// the list and the menu key opening the ways to act on it.
func documentTypesMenu(m *model) submenu {
	types := m.listing.documentTypes

	return submenu{
		kind:  formDocumentTypesMenu,
		title: "document types",
		empty: "no document types",
		entries: xslice.Map(types, func(t *agentv1.DocumentType) submenuEntry {
			return submenuEntry{label: t.GetName()}
		}),
		actions: func(choice int) submenu {
			return documentTypeMenu(m, typeAt(types, choice))
		},
		preview: func(choice, width, height int) string {
			return documentTypePreview(typeAt(types, choice), width, height)
		},
	}
}

func typeAt(types []*agentv1.DocumentType, choice int) *agentv1.DocumentType {
	if choice < 0 || choice >= len(types) {
		return nil
	}

	return types[choice]
}

// documentTypePreview is a type's context as it is drawn beside the list.
func documentTypePreview(t *agentv1.DocumentType, width, height int) string {
	doc := renderDocument("Document types", "_no document types yet_", "", "")
	if t != nil {
		notice := fmt.Sprintf("_version %d, written %s ago_", t.GetVersion(), label.Age(dates.Parse(t.GetUpdatedAt())))
		doc = renderDocument(t.GetName(), notice, documentProse(t.GetName(), t.GetContext()), "")
	}

	rows := paint.DocStyledRows(doc, width, columnStyle())

	return strings.Join(rows[:min(len(rows), height)], "\n")
}

// documentTypeMenu is the actions on the document type under the list's cursor, nil for none.
func documentTypeMenu(m *model, t *agentv1.DocumentType) submenu {
	edit := submenuEntry{label: "edit", keys: m.keys.DocumentTypesMenu.Edit, isOff: true}
	remove := submenuEntry{label: "delete", keys: m.keys.DocumentTypesMenu.Delete, isOff: true}
	title := "document types"
	if t != nil {
		title = t.GetName()
		edit.isOff, edit.form = false, func() formState { return editTypeForm(t) }
		remove.isOff, remove.form = false, func() formState { return deleteTypeForm(documentTypeItem(t)) }
	}

	return submenu{
		kind:  formDocumentTypesMenu,
		title: title,
		entries: []submenuEntry{
			edit,
			{
				label: "new type",
				keys:  m.keys.DocumentTypesMenu.New,
				form:  newTypeForm,
			},
			remove,
		},
	}
}

// documentEntry is one entry of the menu on a document.
type documentEntry struct {
	entry submenuEntry
	run   func(d *documentsScreen, m *model) tea.Cmd
}

func (d *documentsScreen) itemMenu(m *model) (submenu, bool) {
	if d.isDiffFocused(m) {
		return submenu{}, false
	}

	return documentMenu(m, d), true
}

// documentMenu is the actions on the hovered document, and on the documents it is listed among.
func documentMenu(m *model, d *documentsScreen) submenu {
	item, hovered := d.hovered(m)
	title := strings.Join(d.crumbRoot(m), " ")
	if hovered {
		title = item.name
	}

	entries := []documentEntry{
		{
			entry: submenuEntry{
				label: d.editLabel(),
				keys:  m.keys.DocumentMenu.Edit,
				isOff: !hovered && d.editing == nil,
			},
			run: (*documentsScreen).pressEdit,
		},
		{
			entry: submenuEntry{
				label: "rename",
				keys:  m.keys.DocumentMenu.Rename,
				isOff: !hovered || d.editing != nil,
			},
			run: (*documentsScreen).openRename,
		},
		{
			entry: submenuEntry{
				label: "new document",
				keys:  m.keys.DocumentMenu.New,
				isOff: d.editing != nil,
			},
			run: (*documentsScreen).pressNew,
		},
	}

	return submenu{
		kind:  formMenu,
		title: title,
		entries: xslice.Map(entries, func(e documentEntry) submenuEntry {
			entry, run := e.entry, e.run
			entry.open = func(m *model) tea.Cmd {
				m.leaveForm()
				return run(&m.documents, m)
			}

			return entry
		}),
	}
}
