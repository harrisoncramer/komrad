package ui

import (
	"weave/internal/ui/paint"
	"weave/internal/view"
)

// agentsSymbologyView draws the agents list through the view package, which is what
// sidebar.symbology turns on.
func (m model) agentsSymbologyView() string {
	record, help := m.detailsView(), m.renderHelp(m.agentHelp())
	below := record + m.messagesRegion(record, help) + help

	palette := m.palette(regionList)
	rows := m.agentRows()

	list := view.Prepare(agentsSnapshot(snapshotParams{
		rows:        rows,
		marks:       m.listing.marks,
		selected:    m.tree.selected,
		scheduled:   m.scheduledWorkflows(),
		focused:     rowKeyAt(rows, m.tree.cursor),
		caretHidden: m.cursorBlinkedOut(m.isListFocused()),
		matcher:     m.filter.pattern,
		showHidden:  m.tree.showHidden,
		pane:        m.width,
	}))

	return m.headedListView(m.sidebarHeader(palette, list.Columns), view.Rows(list, palette, m.width), m.tree.cursor, below)
}

// sidebarHeader is the title, the search and the row of column names, which a pane too narrow to
// draw a single column leaves out rather than spending a line on nothing.
func (m model) sidebarHeader(palette paint.Palette, columns view.Columns) string {
	header := palette.Title("Agents and Workflows") + "\n\n" + m.filterView()

	names := view.Header(palette, columns)
	if names == "" {
		return header
	}

	return header + names + "\n"
}

// scheduledWorkflows is the workflows at least one schedule launches into, which is all a
// workflow row says about them.
func (m model) scheduledWorkflows() map[string]bool {
	scheduled := make(map[string]bool, len(m.schedules.items))
	for _, item := range m.schedules.items {
		scheduled[item.schedule.WorkflowID] = true
	}

	return scheduled
}
