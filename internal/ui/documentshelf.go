package ui

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// documentShelf is what a documents screen lists: the documents one agent has written, or the
// document types a person defines.
type documentShelf int

const (
	shelfAgentDocuments documentShelf = iota
	shelfDocumentTypes
)

// screen is the screen the shelf is listed on.
func (f documentShelf) screen() screen {
	if f == shelfDocumentTypes {
		return screenDocumentTypes
	}

	return screenDocuments
}

// keys is the shelf's bindings, in the shape both shelves share.
func (f documentShelf) keys(k config.Keybindings) config.DocumentsKeys {
	if f == shelfDocumentTypes {
		t := k.DocumentTypes

		return config.DocumentsKeys{
			Back:   t.Back,
			Edit:   t.Edit,
			Older:  t.Older,
			Newer:  t.Newer,
			New:    t.New,
			Rename: t.Rename,
			Quit:   t.Quit,
		}
	}

	return k.Documents
}

// save writes back what the editor changed in the text of an item.
func (f documentShelf) save(a api, item documentItem, version int32, body string) error {
	if f == shelfDocumentTypes {
		return a.writeDocumentType(item, version, item.name, body)
	}

	return a.writeDocument(item, version, body)
}

// listVersions lists every write kept of an item, the newest first.
func (f documentShelf) listVersions(a api, id string) ([]historyVersion, error) {
	if f == shelfDocumentTypes {
		versions, err := a.listDocumentTypeVersions(id)
		if err != nil {
			return nil, err
		}

		return xslice.Map(versions, func(v *agentv1.DocumentTypeVersion) historyVersion {
			return historyVersion{
				version:   v.GetVersion(),
				name:      v.GetName(),
				createdAt: v.GetCreatedAt(),
			}
		}), nil
	}

	versions, err := a.listDocumentVersions(id)
	if err != nil {
		return nil, err
	}

	return xslice.Map(versions, func(v *agentv1.DocumentVersion) historyVersion {
		by := "the agent"
		if v.GetByPerson() {
			by = "you"
		}

		return historyVersion{
			version:   v.GetVersion(),
			by:        by,
			createdAt: v.GetCreatedAt(),
		}
	}), nil
}

// readVersion reads the text of one write of an item.
func (f documentShelf) readVersion(a api, item documentItem, version int32) (string, error) {
	if f == shelfDocumentTypes {
		documentType, err := a.getDocumentTypeVersion(item.name, version)
		if err != nil {
			return "", err
		}

		return documentType.GetContext(), nil
	}

	document, err := a.getDocumentVersion(item.id, version)
	if err != nil {
		return "", err
	}

	return document.GetBody(), nil
}

// documentsOn is the documents screen listing a shelf, by the screen it is listed on.
func (m *model) documentsOn(on screen) *documentsScreen {
	if on == screenDocumentTypes {
		return &m.documentTypes
	}

	return &m.documents
}

// documentTypesFrom is the document types the watch holds, by name.
func documentTypesFrom(held map[string]*agentv1.DocumentType) []*agentv1.DocumentType {
	types := slices.Collect(maps.Values(held))
	slices.SortFunc(types, func(a, b *agentv1.DocumentType) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.GetName()), strings.ToLower(b.GetName())), strings.Compare(a.GetName(), b.GetName()))
	})

	return types
}

// documentTypeNames is the name of every document type, by name.
func (l listing) documentTypeNames() []string {
	return xslice.Map(l.documentTypes, func(t *agentv1.DocumentType) string { return t.GetName() })
}

func documentTypeItem(t *agentv1.DocumentType) documentItem {
	return documentItem{
		id:        t.GetId(),
		name:      t.GetName(),
		updatedAt: dates.Parse(t.GetUpdatedAt()),
	}
}

// itemsOf is what the shelf lists.
func (d *documentsScreen) itemsOf(s panelShared) []documentItem {
	if d.shelf == shelfDocumentTypes {
		return xslice.Map(s.listing.documentTypes, documentTypeItem)
	}

	return d.items
}

// bodyOf is the text of an item as it was last read, and false while it has not been asked for.
func (d *documentsScreen) bodyOf(s panelShared, id string) (documentBody, bool) {
	if d.shelf != shelfDocumentTypes {
		read, asked := d.bodies[id]

		return read, asked
	}

	for _, t := range s.listing.documentTypes {
		if t.GetId() == id {
			return documentBody{
				updatedAt: dates.Parse(t.GetUpdatedAt()),
				version:   t.GetVersion(),
				body:      t.GetContext(),
			}, true
		}
	}

	return documentBody{}, false
}

// isListed reports whether the shelf's items have come back, which the watch has already done
// for the document types.
func (d *documentsScreen) isListed() bool {
	return d.shelf == shelfDocumentTypes || d.listed
}

// keepCursor puts the cursor back on the row it was on once the rows have moved under it.
func (d *documentsScreen) keepCursor(s panelShared, hovered string) {
	rows := d.rows(s)
	d.cursor = cursor.Place(rowIndexOfKey(rows, hovered), d.cursor, len(rows))
}
