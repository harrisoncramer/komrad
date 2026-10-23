package documents

import (
	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// typeMenu is the actions on the document type under the list's cursor.
func (d *Screen) typeMenu(t *agentv1.DocumentType) menu.Menu {
	return menu.Menu{
		Title: t.GetName(),
		Entries: []menu.Entry{
			{
				Label: "edit",
				Keys:  d.deps.Keys.DocumentTypesMenu.Edit,
				Form:  func() form.Form { return d.editTypeForm(t) },
			},
			{
				Label: "delete",
				Keys:  d.deps.Keys.DocumentTypesMenu.Delete,
				Form:  func() form.Form { return d.deleteTypeForm(typeDocument(t)) },
			},
		},
	}
}

// BuildItemMenu builds the band on the hovered document or type, or on every chosen document while any are selected.
func (d *Screen) BuildItemMenu(in screen.Inputs, nest menu.NestedMenuBuilder) (menu.Menu, bool) {
	if d.kind == kindTypes {
		t := d.hoveredType(in)

		return d.typeMenu(t), t != nil
	}
	if d.isDiffFocused(in) {
		return menu.Menu{}, false
	}

	if d.list().IsSelecting() {
		return d.chosenMenu(in)
	}

	return d.documentMenu(in, nest)
}

// BuildScreenMenu builds the documents screen's own band, for writing a new document or type.
func (d *Screen) BuildScreenMenu(in screen.Inputs, _ menu.NestedMenuBuilder) (menu.Menu, bool) {
	if d.kind == kindTypes {
		return menu.Menu{
			Title: "document types",
			Entries: []menu.Entry{
				{
					Label: "new type",
					Keys:  d.deps.Keys.DocumentTypesMenu.New,
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
				Label:    "new document",
				Keys:     d.deps.Keys.DocumentMenu.New,
				IsDimmed: d.editing != nil || !named,
				Pick:     d.pressNew,
			},
		},
	}, true
}

// documentMenu is the actions on the hovered document, or on the editor open over the list,
// false for neither.
func (d *Screen) documentMenu(in screen.Inputs, nest menu.NestedMenuBuilder) (menu.Menu, bool) {
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
				Keys:  d.deps.Keys.DocumentMenu.Edit,
				Pick:  d.pressEdit,
			},
			{
				Label:    "rename",
				Keys:     d.deps.Keys.DocumentMenu.Rename,
				IsDimmed: !hovered || d.editing != nil,
				Pick:     d.openRename,
			},
			{
				Label:    "delete",
				Keys:     d.deps.Keys.DocumentMenu.Delete,
				IsDimmed: !hovered || d.editing != nil,
				Form:     func() form.Form { return d.deleteForm([]document{item}) },
			},
		},
	}
	if !hovered {
		return band, true
	}

	return menu.WithNested(band, "jump", d.deps.Keys.Menu.Jump, nest.Jump(screen.Documents, screen.Target{AgentID: item.agentID, TaskID: item.taskID})), true
}

// chosenMenu is the actions on every chosen document, false for none.
func (d *Screen) chosenMenu(in screen.Inputs) (menu.Menu, bool) {
	chosen := d.chosen(in)
	if len(chosen) == 0 {
		return menu.Menu{}, false
	}

	return menu.Menu{
		Title: label.Count(len(chosen), "document", "documents"),
		Entries: []menu.Entry{
			{
				Label:    "delete all",
				Keys:     d.deps.Keys.DocumentMenu.Delete,
				IsDimmed: d.editing != nil,
				Form:     func() form.Form { return d.deleteForm(chosen) },
			},
		},
	}, true
}

// chosen is the selected documents the list shows.
func (d *Screen) chosen(in screen.Inputs) []document {
	rows := xslice.Filter(d.rows(in), func(r row) bool { return d.Selected.Has(r.Key()) })

	return xslice.Map(rows, func(r row) document { return r.item })
}
