package ui

import (
	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// documentTypeMenu is the actions on the document type under the list's cursor.
func documentTypeMenu(m *model, t *agentv1.DocumentType) menu.Menu {
	return menu.Menu{
		Title: t.GetName(),
		Entries: []menu.Entry{
			{
				Label: "edit",
				Keys:  m.keys.DocumentTypesMenu.Edit,
				Form:  func() form.Form { return editTypeForm(m, t) },
			},
			{
				Label: "delete",
				Keys:  m.keys.DocumentTypesMenu.Delete,
				Form:  func() form.Form { return deleteTypeForm(m, documentTypeItem(t)) },
			},
		},
	}
}

// documentEntry is one entry of the menu on a document.
type documentEntry struct {
	entry menu.Entry
	run   func(d *documentsScreen, m *model) screen.Request
}

func (d *documentsScreen) buildItemMenu(m *model) (menu.Menu, bool) {
	if d.kind == documentKindTypes {
		t := d.hoveredDocumentType(m)

		return documentTypeMenu(m, t), t != nil
	}
	if d.isDiffFocused(m) {
		return menu.Menu{}, false
	}

	return documentMenu(m, d)
}

// buildScreenMenu builds the documents screen's own band, for writing a new document or type.
func (d *documentsScreen) buildScreenMenu(m *model) (menu.Menu, bool) {
	if d.kind == documentKindTypes {
		return menu.Menu{
			Title: "document types",
			Entries: []menu.Entry{
				{
					Label: "new type",
					Keys:  m.keys.DocumentTypesMenu.New,
					Form:  func() form.Form { return newTypeForm(m) },
				},
			},
		}, true
	}
	if d.isDiffFocused(m) {
		return menu.Menu{}, false
	}

	_, named := m.namedAgentID()

	return menu.Menu{
		Title: "documents",
		Entries: documentEntries(m, []documentEntry{
			{
				entry: menu.Entry{
					Label: "new document",
					Keys:  m.keys.DocumentMenu.New,
					Off:   d.editing != nil || !named,
				},
				run: (*documentsScreen).pressNew,
			},
		}),
	}, true
}

// documentMenu is the actions on the hovered document, or on the editor open over the list,
// false for neither.
func documentMenu(m *model, d *documentsScreen) (menu.Menu, bool) {
	item, hovered := d.hovered(m)
	if !hovered && d.editing == nil {
		return menu.Menu{}, false
	}

	title := "documents"
	if hovered {
		title = item.name
	}

	band := menu.Menu{
		Title: title,
		Entries: documentEntries(m, []documentEntry{
			{
				entry: menu.Entry{
					Label: d.editLabel(),
					Keys:  m.keys.DocumentMenu.Edit,
				},
				run: (*documentsScreen).pressEdit,
			},
			{
				entry: menu.Entry{
					Label: "rename",
					Keys:  m.keys.DocumentMenu.Rename,
					Off:   !hovered || d.editing != nil,
				},
				run: (*documentsScreen).openRename,
			},
		}),
	}
	if !hovered {
		return band, true
	}

	return menu.WithNested(band, "jump", m.keys.Menu.Jump, jumpMenu(m, screen.Documents, screen.Target{AgentID: item.agentID, TaskID: item.taskID})), true
}

// documentEntries are the entries that leave the menu and run on the documents screen.
func documentEntries(m *model, entries []documentEntry) []menu.Entry {
	return xslice.Map(entries, func(e documentEntry) menu.Entry {
		entry, run := e.entry, e.run
		entry.Pick = func() screen.Request { return run(&m.documents, m) }

		return entry
	})
}
