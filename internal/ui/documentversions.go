package ui

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"komrad/internal/ui/cursor"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// listVersions lists every write kept of a document, the newest first.
func listVersions(a api, id string) ([]historyVersion, error) {
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

// readVersion reads the text of one write of a document.
func readVersion(a api, item documentItem, version int32) (string, error) {
	document, err := a.getDocumentVersion(item.id, version)
	if err != nil {
		return "", err
	}

	return document.GetBody(), nil
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

// keepCursor puts the cursor back on the row it was on once the rows have moved under it.
func (d *documentsScreen) keepCursor(s panelShared, hovered string) {
	rows := d.rows(s)
	d.cursor = cursor.Place(rowIndexOfKey(rows, hovered), d.cursor, len(rows))
	d.reconcileRename(s)
}
