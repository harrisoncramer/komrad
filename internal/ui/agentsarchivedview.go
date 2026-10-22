package ui

import (
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
)

func (a *archivedAgents) details(m *model) details {
	r, ok := list.At(a.listRows(m), a.Row)
	if !ok {
		return details{}
	}

	if r.isTask() {
		return taskDetails(m, a.deps.glyphs, r)
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, m.listing.ArchivedCount(r.workflow.ID)),
		}
	}

	return agentDetails(m, a.deps.glyphs, r)
}

// doc is the markdown rendered beside the list for the row the cursor is on.
func (a *archivedAgents) doc(m *model) string {
	r, ok := list.At(a.listRows(m), a.Row)
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
func (a *archivedAgents) docRows(m *model, width int) []string {
	return paint.DocStyledRows(a.doc(m), width, columnStyle())
}

// archiveDocument is what the panel renders beside an archived agent's row.
func archiveDocument(agent panel.Agent) string {
	if agent.State.IsFailed() {
		return failedPlaceholder(agent)
	}

	return archiveMarkdown(agent)
}

func (a *archivedAgents) view(m *model) string {
	return m.columnListView(a.listParams(m))
}

// snapshot is the archived agents' rows as the plain data the shared rendering reads.
func (a *archivedAgents) snapshot(m *model, pane list.Pane) view.Snapshot {
	rows := a.listRows(m)
	work := splitChosen(m.listing, chosenAgents(a.Selected, rows))

	return agentsSnapshot(snapshotParams{
		rows:         rows,
		selected:     a.Selected,
		wholeTasks:   panel.TaskIDs(work.wholeTasks),
		scheduled:    m.listing.ScheduledWorkflows(),
		workflowDirs: m.listing.WorkflowDirs(),
		focused:      list.KeyAt(rows, a.Row),
		caret:        pane.Caret,
		loaderFrame:  m.loading.frame(),
		matcher:      m.filter.pattern,
		picked:       pickedRows(m.filter, rows, m.agentRowFits),
		tasks:        m.listing.Tasks,
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
