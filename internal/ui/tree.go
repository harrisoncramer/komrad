package ui

import (
	agentstore "claude-agents/internal/store/agent"
	workflowstore "claude-agents/internal/store/workflow"
)

// The agents list is a tree of workflow folders with their agents nested beneath, so
// an agent's parent shows by position rather than by a marker on the row. One cursor
// moves over both kinds of row, which is why everything that resolves what is under
// it lives together here.

// treeState is the agents screen. The two listings are held together because they are
// drawn as one tree, and there is one cursor over it: a workflow header and an agent
// are rows of the same list, so the cursor indexes rows rather than either listing.
type treeState struct {
	agents    []agentItem
	workflows []workflowstore.Workflow
	cursor    int

	// focus is an agent the next listing should put the cursor on, consumed as it
	// lands. A restore can hand back a different name than it went in with, and the
	// row under that name only exists once the listing that carries it arrives.
	focus string

	// placed says a listing has put the cursor somewhere. Nothing is listed when the
	// panel paints its first frame, so the first one lands it on the first agent rather
	// than on the workflow header a cursor nobody has placed sits on.
	placed bool

	// showHidden draws the hidden agents alongside the rest. It is the panel's own
	// state rather than anything stored, so it lasts as long as this process and every
	// panel starts with the hidden ones away.
	showHidden bool

	// infoPinned shows the hovered agent's stored record in the stage instead of the
	// agent itself. A live agent otherwise occupies the stage with its own pane, so
	// its cwd and flags have nowhere else to show. Moving the cursor or opening an
	// agent clears it, so the pinned record never outlives the row it describes.
	infoPinned bool
}

// agentRow is one line of the agents list, either a workflow header or an agent
// nested beneath one. The cursor moves over both but actions apply only to agent
// rows.
type agentRow struct {
	header bool
	label  string    // the workflow's name when header is true
	item   agentItem // the agent when header is false
	// workflowIndex is the header's index in m.tree.workflows so workflow actions can
	// target it, and the index of the enclosing workflow for an agent row, so a new
	// agent started from one lands in the same place.
	workflowIndex int
	// hiddenCount is how many of a header's agents are hidden, so the folder can say
	// how much of itself it is not showing. It is always the full count, whether or
	// not the hidden ones are on screen.
	hiddenCount int
}

// agentRows arranges the agents into the displayed tree. Every workflow is a
// header followed by its agents, and an empty workflow still gets a header so a
// new one can be hovered to add its first agent.
//
// Every agent belongs to a workflow, so there is no trailing group for ones that
// do not. An agent whose workflow is not in the listing, which means the
// workflow was deleted, is left out rather than shown under a header that is not
// there.
//
// A hidden agent gives up its row and is counted against its workflow's header
// instead, unless the panel is showing the hidden ones. The filtering happens here
// rather than at the listing, so every other screen and every count still sees the
// whole set.
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

// hoveredWorkflow returns the workflow whose header the cursor is on, or false when
// the cursor is not on a header. It hands back the record rather than its index, since
// the index is only ever used to look the record up again.
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

// enclosingWorkflow is the workflow the cursor is inside, whether it sits on the
// header or on one of the agents under it, so a new agent started from anywhere in the
// tree lands in the workflow it was started from. hoveredWorkflow is the narrower
// question, which only a header answers. It is nil when the tree is empty, which means
// no workflow exists to start one in.
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

// agentItem is a stored agent plus whether its tmux pane is currently alive. A
// dormant agent has a dead pane but is kept, since it can be restored from its stored state.
type agentItem struct {
	agent agentstore.Agent
	alive bool
}

// applyWorkflows takes a freshly-listed set as the on-screen workflows. The listing
// sorts by id, which is fixed at creation, so it reads newest first and no edit can
// move a folder under the cursor. Only a create or a delete changes the tree, and the
// cursor that moves over it is reapplied with the agents.
func (m *model) applyWorkflows(fresh []workflowstore.Workflow) {
	m.tree.workflows = fresh
}

// applyAgents takes a freshly-listed set as the on-screen agents. The listing is
// newest first and holds still while an agent is worked on, so a row only moves when
// an agent above it is created or goes away. The cursor is kept on the agent it was
// on rather than on its index for that reason. The fresh items carry updated state
// and liveness, so a reconciler change shows without a structural reload.
func (m *model) applyAgents(fresh []agentItem) {
	selected := ""
	if item, ok := m.selectedAgent(); ok {
		selected = item.agent.Name
	}

	// A row an action asked to be shown takes the cursor from whatever was under it,
	// for this listing only.
	if m.tree.focus != "" {
		selected, m.tree.focus = m.tree.focus, ""
	}

	m.tree.agents = fresh
	rows := m.agentRows()

	// The cursor indexes the tree of headers and agents, so keep it on the same
	// agent across the rebuild, or clamp it into the new tree when that agent is
	// gone.
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
