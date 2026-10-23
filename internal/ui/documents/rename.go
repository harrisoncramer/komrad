package documents

import (
	"fmt"
	"slices"

	"komrad/internal/ui/api"
	"komrad/internal/ui/field"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// rename is a document's or a type's name as the user types over it in the list.
type rename struct {
	*field.NameEdit
	item document
	read text
}

func (d *Screen) renamingField() *field.NameEdit {
	if d.renaming == nil {
		return nil
	}

	return d.renaming.NameEdit
}

// openRename types over the name of the item under the cursor once its text has been read.
func (d *Screen) openRename(in screen.Inputs) screen.Request {
	item, ok := d.hovered(in)
	if !ok || d.editing != nil {
		return nil
	}

	read, asked := d.bodies[item.id]
	switch {
	case asked && read.err != nil:
		return screen.NewNote(screen.Warning, fmt.Sprintf("%q could not be read, so it cannot be renamed", item.name))
	case !asked || read.version == 0:
		return screen.NewNote(screen.Warning, fmt.Sprintf("%q has not been read yet, so it cannot be renamed", item.name))
	}

	d.renaming = &rename{
		NameEdit: field.NewNameEdit(row{item: item}.Key(), item.name),
		item:     item,
		read:     read,
	}

	return nil
}

// reconcileRename drops an open rename once the cursor leaves its row.
func (d *Screen) reconcileRename(in screen.Inputs) {
	if d.renaming != nil && !field.ReconcileNameEdit(d.renaming.NameEdit, d.rows(in), d.ActiveList().Hovered(in), func(r row) string { return r.item.name }) {
		d.renaming = nil
	}
}

func (d *Screen) updateRename(msg tea.Msg) screen.Request {
	switch d.renaming.Take(msg, d.deps.Register) {
	case field.NameGone:
		return screen.NewNote(screen.Warning, d.renaming.GoneWarning())
	case field.NameHidden:
		return screen.Hide{}
	case field.NameCancelled:
		d.renaming = nil
	case field.NameSubmitted:
		return d.saveRename()
	case field.NameTyping:
	}

	return nil
}

func (d *Screen) saveRename() screen.Request {
	renaming := d.renaming
	name, ok := renaming.Submit()
	if !ok {
		d.renaming = nil

		return nil
	}

	item, read := renaming.item, renaming.read
	if i := slices.IndexFunc(d.items, func(i document) bool { return i.id == item.id }); i >= 0 {
		item = d.items[i]
	}
	if fresh, asked := d.bodies[item.id]; asked && !fresh.pending && fresh.err == nil {
		read = fresh
	}
	item.name = name

	return d.deps.Clients.RenameWrite(api.RenameWriteParams{
		Doing:   "renaming " + renaming.Was(),
		Summary: fmt.Sprintf("Could not rename document %q", renaming.Was()),
		Row:     renaming.Key(),
		Write: func(a api.Clients) error {
			return a.WriteDocument(api.WriteDocumentParams{
				ID:      item.id,
				Version: read.version,
				Name:    item.name,
				Type:    item.typeName,
				Body:    read.body,
			})
		},
	})
}

// settleRename marks the field saved once its write has landed, or open for another try when it
// failed.
func (d *Screen) settleRename(in screen.Inputs, key string, err error) {
	if d.renaming == nil {
		return
	}
	d.renaming.Settle(key, err)

	d.reconcileRename(in)
}

// IsRenaming reports whether a document's name is being typed over.
func (d *Screen) IsRenaming() bool {
	return d.renaming != nil && !d.renaming.IsLanded()
}

// TypedLine is the name being typed over or the diff's search, whichever is typed into.
func (d *Screen) TypedLine() (field.Line, bool) {
	switch {
	case d.IsRenaming():
		return d.renaming.Input(), true
	case d.history != nil && d.history.filter.IsTyping:
		return d.history.filter.Input, true
	}

	return field.Line{}, false
}
