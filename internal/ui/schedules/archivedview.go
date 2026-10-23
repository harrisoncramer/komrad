package schedules

import (
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
)

const emptyArchiveMarkdown = "# Archive\n\n_no archived schedules_\n\nKomrad keeps the schedules you archive here until you delete them.\n"

// Details is the record of the archived schedule the cursor is on.
func (a Archive) Details(in screen.Inputs) record.Details {
	item, ok := list.At(a.listRows(in), a.Row)
	if !ok {
		return record.Details{}
	}

	return details(in.Listing, item)
}

// docRows is the block rendered beside the list at a width for the row the cursor is on.
func (a Archive) docRows(in screen.Inputs, width int) []string {
	item, ok := list.At(a.listRows(in), a.Row)
	if !ok {
		return paint.DocStyledRows(emptyArchiveMarkdown, width, paint.Panel.Stage())
	}

	return documentRows(item, width)
}
