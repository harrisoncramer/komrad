package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// documentRename is a document's or a type's name being typed over in the list.
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
func (d *documentsScreen) openRename(s panelShared) tea.Cmd {
	item, ok := d.hovered(s)
	if !ok || d.editing != nil {
		return nil
	}

	read, asked := d.bodyOf(s, item.id)
	if !asked || read.pending || read.err != nil {
		return nil
	}

	d.renaming = &documentRename{
		nameEdit: openNameEdit(documentRow{item: item}.key(), item.name),
		item:     item,
		read:     read,
	}

	return nil
}

// reconcileRename drops an open rename whose row the cursor is no longer on.
func (d *documentsScreen) reconcileRename(s panelShared) {
	if d.renaming == nil || d.renaming.saving || d.hoveredKey(s) == d.renaming.key {
		return
	}

	d.renaming = nil
}

func (d *documentsScreen) updateRename(h host, msg tea.Msg) tea.Cmd {
	switch d.renaming.take(msg) {
	case nameEditHidden:
		return h.hide()
	case nameEditCancelled:
		d.renaming = nil
	case nameEditSubmitted:
		return d.saveRename(h)
	case nameEditTyping:
	}

	return nil
}

func (d *documentsScreen) saveRename(h host) tea.Cmd {
	rename := d.renaming
	name, ok := rename.submit()
	if !ok {
		d.renaming = nil

		return nil
	}

	item, read := rename.item, rename.read
	if d.shelf == shelfDocumentTypes {
		return stampRename(h.writeCmd(
			fmt.Sprintf("Could not rename document type %q", rename.was),
			func(a api) error { return a.writeDocumentType(item, read.version, name, read.body) },
		), rename.key)
	}

	item.name = name

	return stampRename(h.writeCmd(
		fmt.Sprintf("Could not rename document %q", rename.was),
		func(a api) error { return a.writeDocument(item, read.version, read.body) },
	), rename.key)
}

// settleRename closes the field once its write has landed, and opens it again for another try
// when the write failed.
func (d *documentsScreen) settleRename(s panelShared, row string, err error) {
	if d.renaming == nil {
		return
	}
	if !d.renaming.settle(row, err) {
		d.renaming = nil

		return
	}

	d.reconcileRename(s)
}

func (d *documentsScreen) isRenaming() bool {
	return d.renaming != nil
}
