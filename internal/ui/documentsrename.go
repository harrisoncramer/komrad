package ui

import (
	"fmt"
	"slices"

	"komrad/internal/ui/field"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// documentRename is a document's or a type's name as the user types over it in the list.
type documentRename struct {
	*field.NameEdit
	item documentItem
	read documentBody
}

func (d *documentsScreen) renamingField() *field.NameEdit {
	if d.renaming == nil {
		return nil
	}

	return d.renaming.NameEdit
}

// openRename types over the name of the item under the cursor once its text has been read.
func (d *documentsScreen) openRename(m *model) screen.Request {
	item, ok := d.hovered(m)
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

	d.renaming = &documentRename{
		NameEdit: field.NewNameEdit(documentRow{item: item}.Key(), item.name),
		item:     item,
		read:     read,
	}

	return nil
}

// reconcileRename drops an open rename once the cursor leaves its row.
func (d *documentsScreen) reconcileRename(m *model) {
	if d.renaming != nil && !reconcileNameEdit(d.renaming.NameEdit, d.rows(m), d.activeList().hovered(m), func(r documentRow) string { return r.item.name }) {
		d.renaming = nil
	}
}

func (d *documentsScreen) updateRename(m *model, msg tea.Msg) screen.Request {
	switch d.renaming.Take(msg, m.register) {
	case field.NameGone:
		return screen.NewNote(screen.Warning, d.renaming.GoneWarning())
	case field.NameHidden:
		return screen.Hide{}
	case field.NameCancelled:
		d.renaming = nil
	case field.NameSubmitted:
		return d.saveRename(m)
	case field.NameTyping:
	}

	return nil
}

func (d *documentsScreen) saveRename(m *model) screen.Request {
	rename := d.renaming
	name, ok := rename.Submit()
	if !ok {
		d.renaming = nil

		return nil
	}

	item, read := rename.item, rename.read
	if i := slices.IndexFunc(d.items, func(i documentItem) bool { return i.id == item.id }); i >= 0 {
		item = d.items[i]
	}
	if fresh, asked := d.bodies[item.id]; asked && !fresh.pending && fresh.err == nil {
		read = fresh
	}
	item.name = name

	return m.renameCmd(
		"renaming "+rename.Was(),
		fmt.Sprintf("Could not rename document %q", rename.Was()),
		rename.Key(),
		func(a api) error { return a.writeDocument(item, read.version, read.body) },
	)
}

// settleRename marks the field saved once its write has landed, or open for another try when it
// failed.
func (d *documentsScreen) settleRename(m *model, row string, err error) {
	if d.renaming == nil {
		return
	}
	d.renaming.Settle(row, err)

	d.reconcileRename(m)
}

func (d *documentsScreen) isRenaming() bool {
	return d.renaming != nil && !d.renaming.IsLanded()
}

func (d *documentsScreen) typedLine() (field.Line, bool) {
	switch {
	case d.isRenaming():
		return d.renaming.Input(), true
	case d.history != nil && d.history.filter.typing:
		return d.history.filter.input, true
	}

	return field.Line{}, false
}
