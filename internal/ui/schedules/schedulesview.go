package schedules

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
	schedulev1 "komrad/proto/gen/schedule/v1"
)

const emptyMarkdown = `# Schedules

A schedule launches an agent, or mails an orchestrator, on a cron expression.

Press the new key to write one.
`

// Details is the record of the schedule the cursor is on.
func (w *Screen) Details(in screen.Inputs) record.Details {
	item, ok := w.selected(in)
	if !ok {
		return record.Details{}
	}

	return details(in.Listing, item)
}

func details(listing panel.Listing, item row) record.Details {
	return record.Details{
		ID: item.schedule.ID,
		Fields: recordFields(item.schedule, recordTarget{
			workflow:     workflowByID(listing.Workflows, item.schedule.WorkflowID),
			orchestrator: orchestratorLabel(listing, item.schedule.OrchestratorID),
		}),
	}
}

type recordTarget struct {
	workflow     *panel.Workflow
	orchestrator string
}

func recordFields(sched panel.Schedule, target recordTarget) []record.Field {
	fields := []record.Field{
		{Label: "name", Value: label.OrDash(sched.Name)},
		{Label: "cron", Value: label.OrDash(sched.Cron)},
		{Label: "timezone", Value: label.OrDash(sched.Timezone)},
	}

	if sched.Kind == schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR {
		return append(fields,
			record.Field{Label: "kind", Value: "orchestrator"},
			record.Field{Label: "mails", Value: target.orchestrator},
		)
	}

	if !panel.IsKnownScheduleKind(sched.Kind) {
		return append(fields, record.Field{Label: "kind", Value: "unknown"})
	}

	fields = append(fields, record.Field{Label: "kind", Value: "task"})
	if target.workflow == nil {
		return append(fields, record.Field{Label: "workflow", Value: "— (archived)"})
	}

	fields = append(fields, record.Field{Label: "workflow", Value: target.workflow.Name})

	return append(fields, record.WorkflowConfigFields(*target.workflow)...)
}

// docRows is the document rendered beside the list at a width: the notice for an empty list, or
// the schedule the cursor is on with the firings River holds for it between its next run and
// its prompt.
func (w *Screen) docRows(in screen.Inputs, width int) []string {
	item, ok := w.selected(in)
	if !ok {
		return paint.Panel.DocRows(emptyMarkdown, width)
	}

	return documentRows(item, width)
}

func (w *Screen) snapshot(in screen.Inputs, pane list.Pane) view.Snapshot {
	return listSnapshot(in, pane, listSnapshotParams{
		rows:     w.listRows(in),
		cursor:   w.Row,
		renaming: w.renamingField(),
	})
}

type listSnapshotParams struct {
	rows     []row
	cursor   int
	renaming *field.NameEdit
}

func listSnapshot(in screen.Inputs, pane list.Pane, params listSnapshotParams) view.Snapshot {
	return view.Snapshot{
		Rows: xslice.Map(params.rows, func(item row) view.Row {
			name := rowName(item)
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
				Cells:   []view.Cell{icon(item.schedule.Enabled)},
				Tail:    view.Cell{Text: item.schedule.Cron, Tone: paint.ToneMuted},
			})
		}),
		Caret:   pane.Caret,
		Focused: list.KeyAt(params.rows, params.cursor),
		Pane:    pane.Width,
		Columns: columns(),
	}
}

func columns() view.Columns {
	return view.Columns{
		{Kind: view.ColumnText, Width: glyph.Columns},
	}
}

func icon(enabled bool) view.Cell {
	if enabled {
		return view.Cell{Glyph: glyph.ScheduleEnabled, Tone: tone(enabled)}
	}

	return view.Cell{Glyph: glyph.ScheduleDisabled, Tone: tone(enabled)}
}

func tone(enabled bool) paint.Tone {
	if enabled {
		return paint.ToneSuccess
	}

	return paint.ToneMuted
}

// documentRows is a schedule's document at a width: its name and next run, its firings, then
// its prompt.
func documentRows(item row, width int) []string {
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

	rows := paint.Panel.DocRows(paint.DocumentMarkdown(sched.Name, notice, "", ""), width)
	if fired := runRows(sched.RecentRuns, width); len(fired) > 0 {
		rows = append(append(rows, ""), fired...)
	}

	return append(rows, paint.Panel.DocRows(prose+"\n", width)...)
}
