package ui

import (
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
		rows:       rows,
		marks:      m.listing.marks,
		selected:   m.tree.selected,
		focused:    m.focusedRowKey(rows),
		showHidden: m.tree.showHidden,
		nameWidth:  view.NameColumns(m.width),
	}))

	header := palette.Title("Agents and Workflows") + "\n\n" + m.filterView() + view.Header(palette) + "\n"

	return m.headedListView(header, view.Rows(list, palette, m.width), m.tree.cursor, below)
}

// focusedRowKey is the row the caret stands on, which is none of them while the blink has that
// caret hidden.
func (m model) focusedRowKey(rows []agentRow) string {
	if m.cursorBlinkedOut(m.isListFocused()) {
		return ""
	}

	return rowKeyAt(rows, m.tree.cursor)
}
