package ui

import (
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
)

func (a *archivedSchedules) details(m *model) details {
	item, ok := list.At(a.listRows(m), a.Row)
	if !ok {
		return details{}
	}

	return scheduleDetails(m, item)
}

// docRows is the block rendered beside the list at a width for the row the cursor is on.
func (a *archivedSchedules) docRows(m *model, width int) []string {
	item, ok := list.At(a.listRows(m), a.Row)
	if !ok {
		return paint.DocStyledRows(emptyScheduleArchiveMarkdown, width, columnStyle())
	}

	return scheduleDocRows(item, width)
}

func (a *archivedSchedules) view(m *model) string {
	return m.columnListView(a.listParams(m))
}
