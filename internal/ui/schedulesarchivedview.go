package ui

import (
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
)

func (a *archivedSchedules) details(m *model, in screen.Inputs) record.Details {
	item, ok := list.At(a.listRows(m, in), a.Row)
	if !ok {
		return record.Details{}
	}

	return scheduleDetails(m, item)
}

// docRows is the block rendered beside the list at a width for the row the cursor is on.
func (a *archivedSchedules) docRows(m *model, in screen.Inputs, width int) []string {
	item, ok := list.At(a.listRows(m, in), a.Row)
	if !ok {
		return paint.DocStyledRows(emptyScheduleArchiveMarkdown, width, columnStyle())
	}

	return scheduleDocRows(item, width)
}
