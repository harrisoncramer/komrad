package ui

import (
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
)

func (a *archivedAgents) details(m *model, in screen.Inputs) details {
	r, ok := list.At(a.listRows(m, in), a.Row)
	if !ok {
		return details{}
	}

	if r.isTask() {
		return taskDetails(m, in, a.deps.glyphs, r)
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, in.Listing.ArchivedCount(r.workflow.ID)),
		}
	}

	return agentDetails(m, in, a.deps.glyphs, r)
}

// doc is the markdown rendered beside the list for the row the cursor is on.
func (a *archivedAgents) doc(m *model, in screen.Inputs) string {
	r, ok := list.At(a.listRows(m, in), a.Row)
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
func (a *archivedAgents) docRows(m *model, in screen.Inputs, width int) []string {
	return paint.DocStyledRows(a.doc(m, in), width, columnStyle())
}

// archiveDocument is what the panel renders beside an archived agent's row.
func archiveDocument(agent panel.Agent) string {
	if agent.State.IsFailed() {
		return failedPlaceholder(agent)
	}

	return archiveMarkdown(agent)
}

// snapshot is the archived agents' rows as the plain data the shared rendering reads.
func (a *archivedAgents) snapshot(m *model, in screen.Inputs, pane list.Pane) view.Snapshot {
	rows := a.listRows(m, in)
	work := splitChosen(in.Listing, chosenAgents(a.Selected, rows))

	return agentsSnapshot(snapshotParams{
		rows:         rows,
		selected:     a.Selected,
		wholeTasks:   panel.TaskIDs(work.wholeTasks),
		scheduled:    in.Listing.ScheduledWorkflows(),
		workflowDirs: in.Listing.WorkflowDirs(),
		focused:      list.KeyAt(rows, a.Row),
		caret:        pane.Caret,
		loaderFrame:  in.Room.Spinner,
		matcher:      in.Room.Pattern.Regexp,
		picked:       list.Picked(in.Room.Pattern, rows, agentRowFits(in)),
		tasks:        in.Listing.Tasks,
		renaming:     a.renamingField(),
		showHidden:   true,
		flat:         a.sort.flat,
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
