package agents

import (
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
)

func (a *archivedAgents) Details(in screen.Inputs) record.Details {
	r, ok := list.At(a.listRows(in), a.Row)
	if !ok {
		return record.Details{}
	}

	if r.isTask() {
		return taskDetails(in, a.deps.Clock.Now(), a.deps.Glyphs, r)
	}

	if r.header {
		return record.Details{
			ID:     r.workflow.ID,
			Fields: workflowFields(r.workflow, in.Listing.ArchivedCount(r.workflow.ID)),
		}
	}

	return agentDetails(in, a.deps.Clock.Now(), a.deps.Glyphs, r)
}

// doc is the markdown rendered beside the list for the row the cursor is on.
func (a *archivedAgents) doc(in screen.Inputs) string {
	r, ok := list.At(a.listRows(in), a.Row)
	switch {
	case !ok:
		return a.emptyMarkdown()
	case r.isTask():
		return taskMarkdown(*r.task)
	case r.header:
		return workflowContextMarkdown(r.workflow)
	default:
		return archiveDocument(r.item.Agent)
	}
}

// docRows is the block rendered beside the list at a width for the row the cursor is on.
func (a *archivedAgents) docRows(in screen.Inputs, width int) []string {
	return paint.DocStyledRows(a.doc(in), width, paint.Panel.Stage())
}

// archiveDocument is what the panel renders beside an archived agent's row.
func archiveDocument(agent panel.Agent) string {
	if panel.IsFailed(agent.State) {
		return failedPlaceholder(agent)
	}

	return archiveMarkdown(agent)
}

// snapshot is the archived agents' rows as the plain data the shared rendering reads.
func (a *archivedAgents) snapshot(in screen.Inputs, pane list.Pane) view.Snapshot {
	rows := a.listRows(in)
	work := splitChosen(in.Listing, chosenAgents(a.Selected, rows))

	return agentsSnapshot(snapshotParams{
		rows:         rows,
		selected:     a.Selected,
		wholeTasks:   panel.TaskIDs(work.wholeTasks),
		scheduled:    in.Listing.ScheduledWorkflows(),
		workflowDirs: in.Listing.WorkflowDirs(),
		focused:      list.KeyAt(rows, a.Row),
		caret:        pane.Caret,
		loaderFrame:  in.Room.SpinnerFrame,
		matcher:      in.Room.Pattern.Regexp,
		picked:       list.Picked(in.Room.Pattern, rows, agentRowFits(in)),
		tasks:        in.Listing.Tasks,
		renaming:     a.renamingField(),
		showHidden:   true,
		flat:         a.sort.AreHeadingsOff,
		pane:         pane.Width,
	})
}

// emptySaid is what the list says while this kind lists nothing.
func (a *archivedAgents) emptySaid() string {
	if a.kind == agentsDeleted {
		return "nothing deleted"
	}

	return "nothing archived"
}

// emptyMarkdown is what renders beside the list while this kind lists nothing.
func (a *archivedAgents) emptyMarkdown() string {
	if a.kind == agentsDeleted {
		return emptyDeletedMarkdown
	}

	return emptyArchiveMarkdown
}
