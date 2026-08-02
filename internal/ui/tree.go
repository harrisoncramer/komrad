package ui

import (
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

// treeState is the agents screen, a tree of workflow folders with their agents nested
// beneath. A header and an agent are rows of the same list, so the cursor indexes rows
// rather than either listing.
type treeState struct {
	agents    []agentItem
	workflows []workflowstore.Workflow
	cursor    int

	// focus is an agent the next listing should put the cursor on, consumed as it lands.
	// A restore can hand back a different name than it went in with, and the row under
	// that name only exists once the listing carrying it arrives.
	focus string

	// placed says a listing has put the cursor somewhere, so the first one lands on the
	// first agent rather than on a workflow header.
	placed bool

	// showHidden draws the hidden agents alongside the rest. It is the panel's own state
	// rather than anything stored, so every panel starts with them away.
	showHidden bool

	// infoPinned shows the hovered agent's stored record in the stage instead of the agent
	// itself, which otherwise occupies the stage with its own pane.
	infoPinned bool

	// logsPinned shows the service's log in the stage in place of the row under the
	// cursor, pinned and cleared the way info is.
	logsPinned bool

	// selected are the agents taken into the selection on this tree.
	selected selection
}

// unpin puts the stage back to following the cursor. Both pins show something other than
// the row under the cursor, so a cursor that moves clears them together.
func (t *treeState) unpin() {
	t.infoPinned = false
	t.logsPinned = false
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

// agentRows is the agents screen's tree, the live workflows over the live agents.
func (m model) agentRows() []agentRow {
	return treeRows(m.tree.workflows, m.tree.agents, m.tree.showHidden)
}

// selectedAgent returns the agent under the cursor, or false when the cursor is on
// a workflow header or the list is empty.
func (m model) selectedAgent() (agentItem, bool) {
	rows := m.agentRows()
	if m.tree.cursor < 0 || m.tree.cursor >= len(rows) || rows[m.tree.cursor].header {
		return agentItem{}, false
	}

	return rows[m.tree.cursor].item, true
}

// hoveredWorkflow returns the workflow whose header the cursor is on, or false when the
// cursor is not on a header.
func (m model) hoveredWorkflow() (workflowstore.Workflow, bool) {
	return hoveredHeader(m.agentRows(), m.tree.cursor)
}

// rowAt is the row a cursor is on, or false when it is on nothing. Both trees index their
// rows through it rather than each bounds-checking its own cursor.
func rowAt(rows []agentRow, cursor int) (agentRow, bool) {
	if cursor < 0 || cursor >= len(rows) {
		return agentRow{}, false
	}

	return rows[cursor], true
}

// hoveredHeader returns the workflow the cursor's header names, or false when the cursor
// is not on a header.
func hoveredHeader(rows []agentRow, cursor int) (workflowstore.Workflow, bool) {
	if cursor < 0 || cursor >= len(rows) || !rows[cursor].header {
		return workflowstore.Workflow{}, false
	}

	return rows[cursor].workflow, true
}

// enclosingWorkflow is the workflow the cursor is inside, header or agent alike, so a new
// agent started from anywhere in the tree lands in the workflow it was started from.
func (m model) enclosingWorkflow() *workflowstore.Workflow {
	rows := m.agentRows()
	if m.tree.cursor < 0 || m.tree.cursor >= len(rows) {
		return nil
	}

	return &rows[m.tree.cursor].workflow
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

// rowIndexOfAgentName returns the row of the named agent, or -1 when it is gone, so
// a refresh can keep the cursor on the same agent as the tree is rebuilt.
func rowIndexOfAgentName(rows []agentRow, name string) int {
	for i, r := range rows {
		if !r.header && r.item.agent.Name == name {
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
		return "workflow:" + row.workflow.ID
	}

	return "agent:" + row.item.agent.ID
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
	n := 0
	for _, it := range m.tree.agents {
		if it.agent.WorkflowID == workflowID {
			n++
		}
	}

	return n
}

// agentItem is a stored agent plus whether its tmux pane is currently alive. A dormant
// agent has a dead pane but is kept, since it can be restored from its stored state.
type agentItem struct {
	agent agentstore.Agent
	alive bool
}

// applyWorkflows takes a freshly-listed set as the on-screen workflows. The listing sorts
// by id, fixed at creation, so no edit can move a folder under the cursor.
func (m *model) applyWorkflows(fresh []workflowstore.Workflow) {
	m.tree.workflows = fresh
}

// applyAgents takes a freshly-listed set as the on-screen agents.
func (m *model) applyAgents(fresh []agentItem) {
	m.tree.agents = fresh
}

// placeTreeCursor puts the cursor back on the row selected names, or as near as the rebuilt
// tree allows. Either listing can add or drop rows above the cursor, so it follows the row
// rather than the index that row happened to be drawn at.
//
// The caller reads selected before the pass starts landing, since a cursor cannot be
// placed against a tree that is half this pass and half the last.
func (m *model) placeTreeCursor(selected string) {
	// A row a write asked to be shown takes the cursor for this listing only. It is named
	// rather than keyed, since a restore can hand back a name the panel has no row for yet.
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
		idx = rowIndexOfAgentName(rows, focus)
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
	m.tree.selected = m.tree.selected.pruned(rows)
}
