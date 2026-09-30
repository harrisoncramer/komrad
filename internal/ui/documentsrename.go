package ui

import (
	"fmt"
	"slices"

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

// reconcileRename drops an open rename once the cursor leaves its row.
func (d *documentsScreen) reconcileRename(s panelShared) {
	if d.renaming != nil && !reconcileNameEdit(d.renaming.nameEdit, d.rows(s), d.hoveredKey(s)) {
		d.renaming = nil
	}
}

func (d *documentsScreen) updateRename(h host, s panelShared, msg tea.Msg) tea.Cmd {
	switch d.renaming.take(h, msg) {
	case nameEditHidden:
		return h.hide()
	case nameEditCancelled:
		d.renaming = nil
	case nameEditSubmitted:
		return d.saveRename(h, s)
	case nameEditTyping:
	}

	return nil
}

func (d *documentsScreen) saveRename(h host, s panelShared) tea.Cmd {
	rename := d.renaming
	name, ok := rename.submit()
	if !ok {
		d.renaming = nil

		return nil
	}

	item, read := rename.item, rename.read
	if i := slices.IndexFunc(d.itemsOf(s), func(i documentItem) bool { return i.id == item.id }); i >= 0 {
		item = d.itemsOf(s)[i]
	}
	if fresh, asked := d.bodyOf(s, item.id); asked && !fresh.pending && fresh.err == nil {
		read = fresh
	}
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
