package ui

import (
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"
)

// treeState is the agents screen, a tree of workflow folders with their agents nested
// beneath. A header and an agent are rows of the same list, so the cursor indexes rows
// rather than either listing. Its focus is a row key, since a jump can land on either a
// header or an agent.
type treeState struct {
	rowCursor

	// placed says a listing has put the cursor somewhere, so the first one lands on the
	// first agent rather than on a workflow header.
	placed bool

	// showHidden draws the hidden agents alongside the rest. It is the panel's own state
	// rather than anything stored, so every panel starts with them away.
	showHidden bool
}

// agentRow is one line of a tree, either a workflow header or an agent nested beneath one.
// The cursor moves over both but the per-row keys apply only to agent rows.
type agentRow struct {
	header bool
	item   agentItem // the agent when header is false
	// workflow is the folder the row sits in, the one a header names and the one an agent
	// is nested under, so the keys acting on a workflow have it off the row.
	workflow workflowstore.Workflow
	// hiddenCount is how many of a header's agents are hidden, always the full count
	// whether or not the hidden ones are on screen.
	hiddenCount int
}

// treeRows arranges agents into the displayed tree. Every workflow is a header followed by
// its agents, and an empty workflow still gets a header so it can be hovered to add its
// first. An agent whose workflow is not in the given set was deleted, so it is left out
// rather than shown under a header that is not there.
//
// A hidden agent gives up its row and is counted against its header instead. The filtering
// happens here rather than at the listing, so every other screen still sees the whole set.
//
// The agents screen and the archive each decide which workflows to draw and build their
// rows here, so the two trees cannot drift into different shapes.
func treeRows(workflows []workflowstore.Workflow, agents []agentItem, showHidden bool) []agentRow {
	byWorkflow := make(map[string][]agentItem)
	hiddenCounts := make(map[string]int)
	for _, it := range agents {
		if it.agent.Hidden {
			hiddenCounts[it.agent.WorkflowID]++
			if !showHidden {
				continue
			}
		}

		byWorkflow[it.agent.WorkflowID] = append(byWorkflow[it.agent.WorkflowID], it)
	}

	var rows []agentRow
	for _, w := range workflows {
		rows = append(rows, agentRow{header: true, workflow: w, hiddenCount: hiddenCounts[w.ID]})
		for _, it := range byWorkflow[w.ID] {
			rows = append(rows, agentRow{item: it, workflow: w})
		}
	}

	return rows
}

// agentRows is the agents screen's tree as it is drawn, the live workflows over the live
// agents less whatever the filter holds back.
func (m model) agentRows() []agentRow {
	return m.filter.filterAgentRows(m.allAgentRows())
}

// allAgentRows is the whole tree, filter or no filter. The selection is kept against it, so a
// pattern narrows what is on screen without dropping agents already chosen.
func (m model) allAgentRows() []agentRow {
	return treeRows(m.listing.workflows, m.listing.agents, m.tree.showHidden)
}

// hoveredRow is the row under the cursor on the agents list, a workflow header as readily
// as an agent. It is how anything outside this screen asks what is hovered without reaching
// for the cursor itself.
func (m model) hoveredRow() (agentRow, bool) {
	return rowAt(m.agentRows(), m.tree.cursor)
}

// selectedAgent returns the agent under the cursor, or false when the cursor is on
// a workflow header or the list is empty.
func (m model) selectedAgent() (agentItem, bool) {
	row, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok || row.header {
		return agentItem{}, false
	}

	return row.item, true
}

// hoveredWorkflow returns the workflow whose header the cursor is on, or false when the
// cursor is not on a header.
func (m model) hoveredWorkflow() (workflowstore.Workflow, bool) {
	return hoveredHeader(m.agentRows(), m.tree.cursor)
}

// hoveredHeader returns the workflow the cursor's header names, or false when the cursor
// is not on a header.
func hoveredHeader(rows []agentRow, cursor int) (workflowstore.Workflow, bool) {
	row, ok := rowAt(rows, cursor)
	if !ok || !row.header {
		return workflowstore.Workflow{}, false
	}

	return row.workflow, true
}

// enclosingWorkflow is the workflow the cursor is inside, header or agent alike, so a new
// agent started from anywhere in the tree lands in the workflow it was started from.
func (m model) enclosingWorkflow() *workflowstore.Workflow {
	row, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok {
		return nil
	}

	return &row.workflow
}

// firstAgentRowIndex returns the index of the first agent row, skipping a leading
// header so entering the list lands on an agent rather than a folder label.
func firstAgentRowIndex(rows []agentRow) int {
	for i, r := range rows {
		if !r.header {
			return i
		}
	}

	return 0
}

// rowIndexOfAgentID returns the row the agent is drawn at, or -1 when the tree has none for
// it, which is how a jump to an agent the screen does not hold is answered.
func rowIndexOfAgentID(rows []agentRow, id string) int {
	for i, r := range rows {
		if !r.header && r.item.agent.ID == id {
			return i
		}
	}

	return -1
}

// rowKey identifies a row across listings, a header by its folder and an agent by itself,
// so a cursor can follow the row it was on as the tree is rebuilt. A header is keyed the
// same as an agent, since a folder appearing or going takes every row below it with it.
func rowKey(row agentRow) string {
	if row.header {
		return workflowRowKey(row.workflow.ID)
	}

	return agentRowKey(row.item.agent.ID)
}

// agentRowKey and workflowRowKey are the keys of rows a write asks to be shown, which it
// names by id because the row carrying it only exists once the next listing lands.
func agentRowKey(id string) string {
	return "agent:" + id
}

func workflowRowKey(id string) string {
	return "workflow:" + id
}

// rowKeyAt is the key of the row a cursor is on, or empty when it is on nothing.
func rowKeyAt(rows []agentRow, cursor int) string {
	if cursor < 0 || cursor >= len(rows) {
		return ""
	}

	return rowKey(rows[cursor])
}

// rowIndexOfKey returns the row carrying the key, or -1 when it is gone.
func rowIndexOfKey(rows []agentRow, key string) int {
	if key == "" {
		return -1
	}

	for i, r := range rows {
		if rowKey(r) == key {
			return i
		}
	}

	return -1
}

// agentCount returns how many listed agents belong to the given workflow id.
func (m model) agentCount(workflowID string) int {
	return xslice.Count(m.listing.agents, func(it agentItem) bool { return it.agent.WorkflowID == workflowID })
}

// agentItem is a stored agent plus whether its tmux pane is currently alive and whether
// its mailbox holds mail nobody has read yet, neither of which the row itself stores.
type agentItem struct {
	agent      agentstore.Agent
	alive      bool
	unreadMail bool
}

// applyWorkflows takes a freshly-listed set as the on-screen workflows. The listing sorts
// by id, fixed at creation, so no edit can move a folder under the cursor.
func (m *model) applyWorkflows(fresh []workflowstore.Workflow) {
	m.listing.workflows = fresh
}

// applyAgents takes a freshly-listed set as the on-screen agents.
func (m *model) applyAgents(fresh []agentItem) {
	m.listing.agents = fresh
}

// placeTreeCursor puts the cursor back on the row selected names, or as near as the rebuilt
// tree allows. Either listing can add or drop rows above the cursor, so it follows the row
// rather than the index that row happened to be drawn at.
//
// The caller reads selected before the pass starts landing, since a cursor cannot be
// placed against a tree that is half this pass and half the last.
func (m *model) placeTreeCursor(selected string) {
	// A row a write asked to be shown takes the cursor for this listing only.
	focus := m.tree.focus
	m.tree.focus = ""

	// Nothing was on screen before the first listing, so there is no row to hold and the
	// cursor lands on the first agent rather than on the folder above it.
	placed := m.tree.placed
	if !placed {
		selected = ""
	}

	rows := m.agentRows()

	idx := rowIndexOfKey(rows, selected)
	if focus != "" {
		idx = rowIndexOfKey(rows, focus)
	}

	switch {
	case idx >= 0:
		m.tree.cursor = idx
	case !placed:
		m.tree.cursor = firstAgentRowIndex(rows)
	default:
		m.tree.cursor = clampCursor(m.tree.cursor, len(rows))
	}

	m.tree.placed = true
	m.tree.selected = m.tree.selected.pruned(m.allAgentRows())
}

// listing is the agents and workflows the last refresh brought back, held once for every
// screen that draws them.
type listing struct {
	agents    []agentItem
	workflows []workflowstore.Workflow
}

// revealAgent puts the tree's cursor on an agent, drawing the hidden ones when that is the only
// way it has a row. An agent the tree has none for at all leaves the cursor where it was.
func (m *model) revealAgent(id string) {
	for _, item := range m.listing.agents {
		if item.agent.ID == id && item.agent.Hidden {
			m.tree.showHidden = true
		}
	}

	if idx := rowIndexOfAgentID(m.agentRows(), id); idx >= 0 {
		m.tree.cursor = idx
	}
}
