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
}

// unpin puts the stage back to following the cursor. Both pins show something other than
// the row under the cursor, so a cursor that moves clears them together.
func (t *treeState) unpin() {
	t.infoPinned = false
	t.logsPinned = false
}

// agentRow is one line of the agents list, either a workflow header or an agent nested
// beneath one. The cursor moves over both but the per-row keys apply only to agent rows.
type agentRow struct {
	header bool
	label  string    // the workflow's name when header is true
	item   agentItem // the agent when header is false
	// workflowIndex is the header's index in m.tree.workflows so the workflow keys can
	// target it, and the enclosing workflow's for an agent row, so a new agent started
	// from one lands in the same place.
	workflowIndex int
	// hiddenCount is how many of a header's agents are hidden, always the full count
	// whether or not the hidden ones are on screen.
	hiddenCount int
}

// agentRows arranges the agents into the displayed tree. Every workflow is a header
// followed by its agents, and an empty workflow still gets a header so it can be hovered
// to add its first. An agent whose workflow is not in the listing was deleted, so it is
// left out rather than shown under a header that is not there.
//
// A hidden agent gives up its row and is counted against its header instead. The filtering
// happens here rather than at the listing, so every other screen still sees the whole set.
func (m model) agentRows() []agentRow {
	byWorkflow := make(map[string][]agentItem)
	hiddenCounts := make(map[string]int)
	for _, it := range m.tree.agents {
		if it.agent.Hidden {
			hiddenCounts[it.agent.WorkflowID]++
			if !m.tree.showHidden {
				continue
			}
		}

		byWorkflow[it.agent.WorkflowID] = append(byWorkflow[it.agent.WorkflowID], it)
	}

	var rows []agentRow
	for wi, w := range m.tree.workflows {
		rows = append(rows, agentRow{header: true, label: w.Name, workflowIndex: wi, hiddenCount: hiddenCounts[w.ID]})
		for _, it := range byWorkflow[w.ID] {
			rows = append(rows, agentRow{item: it, workflowIndex: wi})
		}
	}

	return rows
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
	rows := m.agentRows()
	if m.tree.cursor < 0 || m.tree.cursor >= len(rows) {
		return workflowstore.Workflow{}, false
	}

	r := rows[m.tree.cursor]
	if !r.header || r.workflowIndex < 0 || r.workflowIndex >= len(m.tree.workflows) {
		return workflowstore.Workflow{}, false
	}

	return m.tree.workflows[r.workflowIndex], true
}

// enclosingWorkflow is the workflow the cursor is inside, header or agent alike, so a new
// agent started from anywhere in the tree lands in the workflow it was started from.
func (m model) enclosingWorkflow() *workflowstore.Workflow {
	rows := m.agentRows()
	if m.tree.cursor < 0 || m.tree.cursor >= len(rows) {
		return nil
	}

	idx := rows[m.tree.cursor].workflowIndex
	if idx < 0 || idx >= len(m.tree.workflows) {
		return nil
	}

	workflow := m.tree.workflows[idx]

	return &workflow
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

// applyAgents takes a freshly-listed set as the on-screen agents, keeping the cursor on the
// agent it was on rather than its index, since an agent above it may have gone.
func (m *model) applyAgents(fresh []agentItem) {
	selected := ""
	if item, ok := m.selectedAgent(); ok {
		selected = item.agent.Name
	}

	// A row a write asked to be shown takes the cursor for this listing only.
	if m.tree.focus != "" {
		selected, m.tree.focus = m.tree.focus, ""
	}

	m.tree.agents = fresh
	rows := m.agentRows()

	switch idx := rowIndexOfAgentName(rows, selected); {
	case idx >= 0:
		m.tree.cursor = idx
	case !m.tree.placed:
		m.tree.cursor = firstAgentRowIndex(rows)
	default:
		m.tree.cursor = clampCursor(m.tree.cursor, len(rows))
	}

	m.tree.placed = true
}
