package ui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"
)

// documentRename is a document's or a type's name as the user types over it in the list.
type documentRename struct {
	*nameEdit
	item documentItem
	read documentBody
}

func (d *documentsScreen) renamingField() *nameEdit {
	if d.renaming == nil {
		return nil
	}

	return d.renaming.nameEdit
}

// openRename types over the name of the item under the cursor once its text has been read.
func (d *documentsScreen) openRename(m *model) tea.Cmd {
	item, ok := d.hovered(m)
	if !ok || d.editing != nil {
		return nil
	}

	read, asked := d.bodies[item.id]
	switch {
	case asked && read.err != nil:
		m.note(messageWarning, fmt.Sprintf("%q could not be read, so it cannot be renamed", item.name))

		return nil
	case !asked || read.version == 0:
		m.note(messageWarning, fmt.Sprintf("%q has not been read yet, so it cannot be renamed", item.name))

		return nil
	}

	d.renaming = &documentRename{
		nameEdit: openNameEdit(documentRow{item: item}.key(), item.name),
		item:     item,
		read:     read,
	}

	return nil
}

// reconcileRename drops an open rename once the cursor leaves its row.
func (d *documentsScreen) reconcileRename(m *model) {
	if d.renaming != nil && !reconcileNameEdit(d.renaming.nameEdit, d.rows(m), d.activeList().hovered(m), func(r documentRow) string { return r.item.name }) {
		d.renaming = nil
	}
}

func (d *documentsScreen) updateRename(m *model, msg tea.Msg) tea.Cmd {
	switch d.renaming.take(m, msg) {
	case nameEditHidden:
		return m.hide()
	case nameEditCancelled:
		d.renaming = nil
	case nameEditSubmitted:
		return d.saveRename(m)
	case nameEditTyping:
	}

	return nil
}

func (d *documentsScreen) saveRename(m *model) tea.Cmd {
	rename := d.renaming
	name, ok := rename.submit()
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

	return stampRename(m.writeCmd(
		"renaming "+rename.was,
		fmt.Sprintf("Could not rename document %q", rename.was),
		func(a api) error { return a.writeDocument(item, read.version, read.body) },
	), rename.key)
}

// settleRename marks the field saved once its write has landed, or open for another try when it
// failed.
func (d *documentsScreen) settleRename(m *model, row string, err error) {
	if d.renaming == nil {
		return
	}
	d.renaming.settle(row, err)

	d.reconcileRename(m)
}

func (d *documentsScreen) isRenaming() bool {
	return d.renaming != nil && !d.renaming.landed
}

func (d *documentsScreen) typedLine() (lineInput, bool) {
	switch {
	case d.isRenaming():
		return d.renaming.input, true
	case d.history != nil && d.history.filter.typing:
		return d.history.filter.input, true
	}

	return lineInput{}, false
}
