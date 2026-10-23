package documents

import (
	"komrad/internal/ui/api"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// listVersions lists every write kept of a document, the newest first.
func listVersions(a api.Clients, id string) ([]historyVersion, error) {
	versions, err := a.ListDocumentVersions(id)
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
			createdAt: dates.Read(v.GetCreatedAt()),
		}
	}), nil
}

// readVersion reads the text of one write of a document.
func readVersion(a api.Clients, item document, version int32) (string, error) {
	doc, err := a.GetDocumentVersion(item.id, version)
	if err != nil {
		return "", err
	}

	return doc.GetBody(), nil
}

func typeDocument(t *agentv1.DocumentType) document {
	return document{
		id:        t.GetId(),
		name:      t.GetName(),
		updatedAt: dates.Read(t.GetUpdatedAt()),
	}
}

// keepCursor puts the cursor back on the row it was on once the rows have moved under it.
func (d *Screen) keepCursor(in screen.Inputs, hovered string) {
	rows := d.rows(in)
	d.Row = cursor.Place(list.IndexOf(rows, hovered), d.Row, len(rows))
	d.reconcileRename(in)
}
