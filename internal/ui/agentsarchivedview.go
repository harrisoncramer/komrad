package ui

import (
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
)

func (a *archivedAgents) details(m *model) details {
	r, ok := rowAt(a.listRows(m), a.cursor)
	if !ok {
		return details{}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, m.listing.archivedCount(r.workflow.ID)),
		}
	}

	return agentDetails(m, r)
}

// doc is the markdown rendered beside the list for the row the cursor is on.
func (a *archivedAgents) doc(m *model) string {
	r, ok := rowAt(a.listRows(m), a.cursor)
	switch {
	case !ok:
		return emptyArchiveMarkdown
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
func (a *archivedAgents) snapshot(m *model, pane listPane) view.Snapshot {
	rows := a.listRows(m)

	return agentsSnapshot(snapshotParams{
		rows:         rows,
		selected:     a.selected,
		scheduled:    m.listing.scheduledWorkflows(),
		workflowDirs: m.listing.workflowDirs(),
		focused:      rowKeyAt(rows, a.cursor),
		caret:        pane.caret,
		loaderFrame:  m.loading.frame(),
		matcher:      m.filter.pattern,
		picked:       pickedRows(m.filter, rows, m.agentRowFits),
		renaming:     a.renamingField(),
		showHidden:   true,
		flat:         a.sort.flat,
		pane:         pane.width,
	})
}
