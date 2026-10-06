package ui

import (
	"strings"

	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// documentTypeMenu is the actions on the document type under the list's cursor, nil for none.
func documentTypeMenu(m *model, t *agentv1.DocumentType) submenu {
	edit := submenuEntry{
		label: "edit",
		keys:  m.keys.DocumentTypesMenu.Edit,
		isOff: true,
	}
	remove := submenuEntry{
		label: "delete",
		keys:  m.keys.DocumentTypesMenu.Delete,
		isOff: true,
	}
	title := "document types"
	if t != nil {
		title = t.GetName()
		edit.isOff, edit.form = false, func() formState { return editTypeForm(t) }
		remove.isOff, remove.form = false, func() formState { return deleteTypeForm(documentTypeItem(t)) }
	}

	return submenu{
		kind:  formMenu,
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

func (d *documentsScreen) buildItemMenu(m *model) (submenu, bool) {
	if d.kind == documentKindTypes {
		return documentTypeMenu(m, d.hoveredDocumentType(m)), true
	}
	if d.isDiffFocused(m) {
		return submenu{}, false
	}

	return documentMenu(m, d), true
}

// documentMenu is the actions on the hovered document, and on the documents it is listed among.
func documentMenu(m *model, d *documentsScreen) submenu {
	item, hovered := d.hovered(m)
	title := "documents"
	if root := d.crumbRoot(m); root != nil {
		title = strings.Join(root, " ")
	}
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
				isOff: d.editing != nil || d.listsEveryAgent(),
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
