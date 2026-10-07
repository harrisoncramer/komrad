package ui

import (
	"komrad/internal/ui/paint"
	"komrad/internal/view"
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

// lines is the list as rendered: each row's cursor, icon, name and cron, or one line over no rows.
func (w *schedulesScreen) lines(m *model, pane listPane) []string {
	return scheduleLines(m, pane, scheduleLinesParams{
		rows:     w.listRows(m),
		cursor:   w.cursor,
		idle:     "no schedules yet",
		renaming: w.renamingField(),
	})
}

type scheduleLinesParams struct {
	rows     []scheduleItem
	cursor   int
	idle     string
	renaming *nameEdit
}

// scheduleLines renders schedule rows with the cursor on one, or the idle line over none.
func scheduleLines(m *model, pane listPane, params scheduleLinesParams) []string {
	if len(params.rows) == 0 {
		return []string{dimStyle().Render("  " + emptyLineOf(m.filter, params.idle))}
	}

	rows := make([]string, 0, len(params.rows))
	for i, item := range params.rows {
		selected := i == params.cursor
		prefix := "  "
		if selected {
			prefix = caretGlyph(pane.caret)
		}

		style := nameStyle(selected && pane.caret != view.CaretAbsent, !item.schedule.Enabled)
		name := m.filter.highlight(paint.Panel, clipName(item.schedule.Name), style)
		if e := params.renaming; e != nil && e.key == item.key() {
			name = renderedName(e, style)
		}
		icon := paint.Panel.Style(scheduleTone(item.schedule.Enabled)).Render(scheduleIcon(m.glyphs, item.schedule.Enabled))
		rows = append(rows, prefix+"  "+icon+" "+name+"  "+dimStyle().Render(item.schedule.Cron))
	}

	return rows
}
