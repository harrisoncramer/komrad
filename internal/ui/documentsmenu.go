package ui

import (
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// documentTypeMenu is the actions on the document type under the list's cursor.
func documentTypeMenu(m *model, t *agentv1.DocumentType) submenu {
	return submenu{
		kind:  formMenu,
		title: t.GetName(),
		entries: []submenuEntry{
			{
				label: "edit",
				keys:  m.keys.DocumentTypesMenu.Edit,
				form:  func() formState { return editTypeForm(t) },
			},
			{
				label: "delete",
				keys:  m.keys.DocumentTypesMenu.Delete,
				form:  func() formState { return deleteTypeForm(documentTypeItem(t)) },
			},
		},
	}
}

// documentEntry is one entry of the menu on a document.
type documentEntry struct {
	entry submenuEntry
	run   func(d *documentsScreen, m *model) tea.Cmd
}

func (d *documentsScreen) buildItemMenu(m *model) (submenu, bool) {
	if d.kind == documentKindTypes {
		t := d.hoveredDocumentType(m)

		return documentTypeMenu(m, t), t != nil
	}
	if d.isDiffFocused(m) {
		return submenu{}, false
	}

	return documentMenu(m, d)
}

// buildScreenMenu builds the documents screen's own band, for writing a new document or type.
func (d *documentsScreen) buildScreenMenu(m *model) (submenu, bool) {
	if d.kind == documentKindTypes {
		return submenu{
			kind:  formMenu,
			title: "document types",
			entries: []submenuEntry{
				{
					label: "new type",
					keys:  m.keys.DocumentTypesMenu.New,
					form:  newTypeForm,
				},
			},
		}, true
	}
	if d.isDiffFocused(m) {
		return submenu{}, false
	}

	_, named := m.namedAgentID()

	return submenu{
		kind:  formMenu,
		title: "documents",
		entries: documentEntries([]documentEntry{
			{
				entry: submenuEntry{
					label: "new document",
					keys:  m.keys.DocumentMenu.New,
					isOff: d.editing != nil || !named,
				},
				run: (*documentsScreen).pressNew,
			},
		}),
	}, true
}

// documentMenu is the actions on the hovered document, or on the editor open over the list,
// false for neither.
func documentMenu(m *model, d *documentsScreen) (submenu, bool) {
	item, hovered := d.hovered(m)
	if !hovered && d.editing == nil {
		return submenu{}, false
	}

	title := "documents"
	if hovered {
		title = item.name
	}

	menu := submenu{
		kind:  formMenu,
		title: title,
		entries: documentEntries([]documentEntry{
			{
				entry: submenuEntry{
					label: d.editLabel(),
					keys:  m.keys.DocumentMenu.Edit,
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
		}),
	}
	if !hovered {
		return menu, true
	}

	return withNested(menu, "jump", m.keys.Menu.Jump, jumpMenu(m, screen.Documents, screen.Target{AgentID: item.agentID, TaskID: item.taskID})), true
}

// documentEntries are the entries that leave the menu and run on the documents screen.
func documentEntries(entries []documentEntry) []submenuEntry {
	return xslice.Map(entries, func(e documentEntry) submenuEntry {
		entry, run := e.entry, e.run
		entry.open = func(m *model) tea.Cmd {
			m.leaveForm()
			return run(&m.documents, m)
		}

		return entry
	})
}
