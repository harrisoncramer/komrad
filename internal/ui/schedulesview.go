package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
	"komrad/lib/xslice"
)

func (w *schedulesScreen) details(m *model) details {
	item, ok := w.selected(m)
	if !ok {
		return details{}
	}

	return scheduleDetails(m, item)
}

func scheduleDetails(m *model, item scheduleItem) details {
	return details{
		id: item.schedule.ID,
		fields: scheduleFields(item.schedule, scheduleTarget{
			workflow:     workflowByID(m.listing.workflows, item.schedule.WorkflowID),
			orchestrator: m.listing.orchestratorLabel(item.schedule.OrchestratorID),
		}),
	}
}

// docRows is the document rendered beside the list at a width: the notice for an empty list, or
// the schedule the cursor is on with the firings River holds for it between its next run and
// its prompt.
func (w *schedulesScreen) docRows(m *model, width int) []string {
	item, ok := w.selected(m)
	if !ok {
		return paint.DocStyledRows(noSchedulesMarkdown, width, columnStyle())
	}

	return scheduleDocRows(item, width)
}

func (w *schedulesScreen) view(m *model) string {
	return m.columnListView(w.listParams(m))
}

func (w *schedulesScreen) snapshot(m *model, pane listPane) view.Snapshot {
	return scheduleSnapshot(m, pane, scheduleSnapshotParams{
		rows:     w.listRows(m),
		cursor:   w.cursor,
		renaming: w.renamingField(),
	})
}

type scheduleSnapshotParams struct {
	rows     []scheduleItem
	cursor   int
	renaming *nameEdit
}

func scheduleSnapshot(m *model, pane listPane, params scheduleSnapshotParams) view.Snapshot {
	return view.Snapshot{
		Rows: xslice.Map(params.rows, func(item scheduleItem) view.Row {
			name := scheduleItemName(item)
			process := view.ProcessNone
			if !item.schedule.Enabled {
				process = view.ProcessStopped
			}

			return params.renaming.renderedOver(view.Row{
				Key:     item.key(),
				Kind:    view.KindResult,
				Name:    name,
				Matches: matchesIn(m.filter.pattern, name),
				Process: process,
				Cells:   []view.Cell{scheduleIcon(item.schedule.Enabled)},
				Tail:    view.Cell{Text: item.schedule.Cron, Tone: paint.ToneMuted},
			})
		}),
		Caret:   pane.caret,
		Focused: rowKeyAt(params.rows, params.cursor),
		Pane:    pane.width,
		Columns: scheduleColumns(),
	}
}

func scheduleColumns() view.Columns {
	return view.Columns{
		{Kind: view.ColumnText, Width: glyph.Columns},
	}
}
