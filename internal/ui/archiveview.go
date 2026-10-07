package ui

import (
	"komrad/internal/ui/paint"
)

func (a *archiveScreen) details(m *model) details {
	item, ok := rowAt(a.scheduleListRows(m), a.schedules.cursor)
	if !ok {
		return details{}
	}

	return scheduleDetails(m, item)
}

// docRows is the block drawn beside the list at a width for the row the cursor is on.
func (a *archiveScreen) docRows(m *model, width int) []string {
	item, ok := rowAt(a.scheduleListRows(m), a.schedules.cursor)
	if !ok {
		return paint.DocStyledRows(emptyScheduleArchiveMarkdown, width, columnStyle())
	}

	return scheduleDocRows(item, width)
}

func (a *archiveScreen) view(m *model) string {
	return m.columnListView(a.listParams(m))
}
