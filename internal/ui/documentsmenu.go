package ui

import (
	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"
)

// documentTypeMenu is the actions on the document type under the list's cursor.
func (d *documentsScreen) documentTypeMenu(t *agentv1.DocumentType) menu.Menu {
	return menu.Menu{
		Title: t.GetName(),
		Entries: []menu.Entry{
			{
				Label: "edit",
				Keys:  d.deps.keys.DocumentTypesMenu.Edit,
				Form:  func() form.Form { return d.editTypeForm(t) },
			},
			{
				Label: "delete",
				Keys:  d.deps.keys.DocumentTypesMenu.Delete,
				Form:  func() form.Form { return d.deleteTypeForm(documentTypeItem(t)) },
			},
		},
	}
}

func (d *documentsScreen) buildItemMenu(in screen.Inputs, nest menu.Nester) (menu.Menu, bool) {
	if d.kind == documentKindTypes {
		t := d.hoveredDocumentType(in)

		return d.documentTypeMenu(t), t != nil
	}
	if d.isDiffFocused(in) {
		return menu.Menu{}, false
	}

	return d.documentMenu(in, nest)
}

// buildScreenMenu builds the documents screen's own band, for writing a new document or type.
func (d *documentsScreen) buildScreenMenu(in screen.Inputs, _ menu.Nester) (menu.Menu, bool) {
	if d.kind == documentKindTypes {
		return menu.Menu{
			Title: "document types",
			Entries: []menu.Entry{
				{
					Label: "new type",
					Keys:  d.deps.keys.DocumentTypesMenu.New,
					Form:  d.newTypeForm,
				},
			},
		}, true
	}
	if d.isDiffFocused(in) {
		return menu.Menu{}, false
	}

	_, named := d.namedAgentID(in)

	return menu.Menu{
		Title: "documents",
		Entries: []menu.Entry{
			{
				Label: "new document",
				Keys:  d.deps.keys.DocumentMenu.New,
				Off:   d.editing != nil || !named,
				Pick:  d.pressNew,
			},
		},
	}, true
}

// documentMenu is the actions on the hovered document, or on the editor open over the list,
// false for neither.
func (d *documentsScreen) documentMenu(in screen.Inputs, nest menu.Nester) (menu.Menu, bool) {
	item, hovered := d.hovered(in)
	if !hovered && d.editing == nil {
		return menu.Menu{}, false
	}

	title := "documents"
	if hovered {
		title = item.name
	}

	band := menu.Menu{
		Title: title,
		Entries: []menu.Entry{
			{
				Label: d.editLabel(),
				Keys:  d.deps.keys.DocumentMenu.Edit,
				Pick:  d.pressEdit,
			},
			{
				Label: "rename",
				Keys:  d.deps.keys.DocumentMenu.Rename,
				Off:   !hovered || d.editing != nil,
				Pick:  d.openRename,
			},
		},
	}
	if !hovered {
		return band, true
	}

	return menu.WithNested(band, "jump", d.deps.keys.Menu.Jump, nest.Jump(screen.Documents, screen.Target{AgentID: item.agentID, TaskID: item.taskID})), true
}
