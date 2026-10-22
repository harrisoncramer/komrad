package ui

import (
	"fmt"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/field"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
	"komrad/lib/xslice"
)

func (w *schedulesScreen) Details(in screen.Inputs) record.Details {
	item, ok := w.selected(in)
	if !ok {
		return record.Details{}
	}

	return scheduleDetails(in.Listing, item)
}

func scheduleDetails(listing panel.Listing, item scheduleItem) record.Details {
	return record.Details{
		ID: item.schedule.ID,
		Fields: scheduleFields(item.schedule, scheduleTarget{
			workflow:     workflowByID(listing.Workflows, item.schedule.WorkflowID),
			orchestrator: orchestratorLabel(listing, item.schedule.OrchestratorID),
		}),
	}
}

// docRows is the document rendered beside the list at a width: the notice for an empty list, or
// the schedule the cursor is on with the firings River holds for it between its next run and
// its prompt.
func (w *schedulesScreen) docRows(in screen.Inputs, width int) []string {
	item, ok := w.selected(in)
	if !ok {
		return paint.DocStyledRows(noSchedulesMarkdown, width, columnStyle())
	}

	return scheduleDocRows(item, width)
}

func (w *schedulesScreen) snapshot(in screen.Inputs, pane list.Pane) view.Snapshot {
	return scheduleSnapshot(in, pane, scheduleSnapshotParams{
		rows:     w.listRows(in),
		cursor:   w.Row,
		renaming: w.renamingField(),
	})
}

type scheduleSnapshotParams struct {
	rows     []scheduleItem
	cursor   int
	renaming *field.NameEdit
}

func scheduleSnapshot(in screen.Inputs, pane list.Pane, params scheduleSnapshotParams) view.Snapshot {
	return view.Snapshot{
		Rows: xslice.Map(params.rows, func(item scheduleItem) view.Row {
			name := scheduleItemName(item)
			process := view.ProcessNone
			if !item.schedule.Enabled {
				process = view.ProcessStopped
			}

			return params.renaming.RenderedOver(view.Row{
				Key:     item.Key(),
				Kind:    view.KindResult,
				Name:    name,
				Matches: list.Matches(in.Room.Pattern.Regexp, name),
				Process: process,
				Cells:   []view.Cell{scheduleIcon(item.schedule.Enabled)},
				Tail:    view.Cell{Text: item.schedule.Cron, Tone: paint.ToneMuted},
			})
		}),
		Caret:   pane.Caret,
		Focused: list.KeyAt(params.rows, params.cursor),
		Pane:    pane.Width,
		Columns: scheduleColumns(),
	}
}

func scheduleColumns() view.Columns {
	return view.Columns{
		{Kind: view.ColumnText, Width: glyph.Columns},
	}
}

// scheduleDocRows is a schedule's document at a width: its name and next run, its firings, then
// its prompt.
func scheduleDocRows(item scheduleItem, width int) []string {
	sched := item.schedule

	notice := "_paused — will not run_"
	switch {
	case sched.ArchivedAt != nil:
		notice = "_archived — kept until deleted_"
	case sched.Enabled:
		notice = fmt.Sprintf("_next run %s_", label.OrDash(label.Friendly(item.schedule.NextRun)))
	}

	prose := "_no prompt set_"
	if strings.TrimSpace(sched.Prompt) != "" {
		prose = sched.Prompt
	}

	if sched.HideAgents {
		prose += "\n\nIts runs start hidden."
	}

	rows := paint.DocStyledRows(paint.RenderDocument(sched.Name, notice, "", ""), width, columnStyle())
	if runRows := scheduleRunRows(sched.RecentRuns, width); len(runRows) > 0 {
		rows = append(append(rows, ""), runRows...)
	}

	return append(rows, paint.DocStyledRows(prose+"\n", width, columnStyle())...)
}
