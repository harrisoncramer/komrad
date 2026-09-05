package ui

import (
	"maps"
	"strings"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"
)

type treeState struct {
	rowCursor
	placed     bool
	showHidden bool
}

type agentRow struct {
	header bool
	item   agentItem
	// hiddenCount is the rows this one stands in for: a workflow header counts the hidden
	// agents under it, and a parent counts its own hidden children.
	hiddenCount int
	workflow    workflowstore.Workflow
	// child says the row is drawn under the one above it rather than under the workflow, which
	// is an indent and nothing else: it is an agent row like any other.
	child bool
}

func treeRows(workflows []workflowstore.Workflow, agents []agentItem, showHidden bool) []agentRow {
	children := make(map[string][]agentItem)
	present := make(map[string]bool, len(agents))
	for _, it := range agents {
		present[it.agent.ID] = true
		if it.agent.ParentAgentID != nil {
			children[*it.agent.ParentAgentID] = append(children[*it.agent.ParentAgentID], it)
		}
	}

	byWorkflow := make(map[string][]agentRow)
	hiddenCounts := make(map[string]int)
	for _, it := range agents {
		// A child is drawn where its parent is, and one whose parent is not in this listing has
		// nowhere to be indented under, so it stands on its own rather than going missing.
		if it.agent.ParentAgentID != nil && present[*it.agent.ParentAgentID] {
			continue
		}

		workflowID := it.agent.WorkflowID
		family := append([]agentItem{it}, children[it.agent.ID]...)
		if it.agent.Hidden {
			// Hiding the head takes the rows under it too, so the whole family stands in the
			// workflow's own count.
			hiddenCounts[workflowID] += len(family)
			if !showHidden {
				continue
			}
		}

		rows := byWorkflow[workflowID]
		head := len(rows)
		for _, member := range family {
			// A hidden child leaves its count on the parent drawn above it rather than on the
			// workflow, since that row is where a person went looking for it.
			if member.agent.Hidden && member.agent.ID != it.agent.ID {
				rows[head].hiddenCount++
				if !showHidden {
					continue
				}
			}

			rows = append(rows, agentRow{
				item: member,
				// The indent says which row a child hangs off, so one standing at the head of
				// its own family hangs off nothing.
				child: member.agent.ID != it.agent.ID,
			})
		}
		byWorkflow[workflowID] = rows
	}

	var rows []agentRow
	for _, w := range workflows {
		rows = append(rows, agentRow{
			header:      true,
			workflow:    w,
			hiddenCount: hiddenCounts[w.ID],
		})
		for _, row := range byWorkflow[w.ID] {
			row.workflow = w
			rows = append(rows, row)
		}
	}

	return rows
}

func (m model) agentRows() []agentRow {
	return m.filter.filterAgentRows(m.allAgentRows())
}

func (m model) allAgentRows() []agentRow {
	return treeRows(m.listing.workflows, m.listing.agents, m.tree.showHidden)
}

func (m model) hoveredRow() (agentRow, bool) {
	return rowAt(m.agentRows(), m.tree.cursor)
}

func (m model) selectedAgent() (agentItem, bool) {
	row, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok || row.header {
		return agentItem{}, false
	}

	return row.item, true
}

func (m model) hoveredWorkflow() (workflowstore.Workflow, bool) {
	return hoveredHeader(m.agentRows(), m.tree.cursor)
}

func hoveredHeader(rows []agentRow, cursor int) (workflowstore.Workflow, bool) {
	row, ok := rowAt(rows, cursor)
	if !ok || !row.header {
		return workflowstore.Workflow{}, false
	}

	return row.workflow, true
}

func (m model) enclosingWorkflow() *workflowstore.Workflow {
	row, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok {
		return nil
	}

	return &row.workflow
}

func firstAgentRowIndex(rows []agentRow) int {
	for i, r := range rows {
		if !r.header {
			return i
		}
	}

	return 0
}

// nearestAgentRowIndex steers an index off a header when the row it used to point at
// was an agent that just vanished, so the cursor doesn't jump to an unrelated workflow's
// header just because that header now sits at the same numeric position.
func nearestAgentRowIndex(rows []agentRow, idx int) int {
	row, ok := rowAt(rows, idx)
	if !ok || !row.header {
		return idx
	}

	for i := idx + 1; i < len(rows); i++ {
		if !rows[i].header {
			return i
		}
	}
	for i := idx - 1; i >= 0; i-- {
		if !rows[i].header {
			return i
		}
	}

	return idx
}

func rowIndexOfAgentID(rows []agentRow, id string) int {
	for i, r := range rows {
		if !r.header && r.item.agent.ID == id {
			return i
		}
	}

	return -1
}

func rowKey(row agentRow) string {
	if row.header {
		return workflowRowKey(row.workflow.ID)
	}

	return agentRowKey(row.item.agent.ID)
}

func agentRowKey(id string) string {
	return "agent:" + id
}

func workflowRowKey(id string) string {
	return "workflow:" + id
}

func rowKeyAt(rows []agentRow, cursor int) string {
	if cursor < 0 || cursor >= len(rows) {
		return ""
	}

	return rowKey(rows[cursor])
}

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

// unnamedChildren counts the children the named agents hold that are not among them, taking the
// pool from the screen doing the asking, since the archive holds families the live listing does
// not.
func unnamedChildren(pool, named []agentstore.Agent) int {
	held := make(map[string]bool, len(named))
	for _, agent := range named {
		held[agent.ID] = true
	}

	return xslice.Count(pool, func(agent agentstore.Agent) bool {
		return agent.ParentAgentID != nil && held[*agent.ParentAgentID] && !held[agent.ID]
	})
}

// liveAgents is the listing as bare rows, for what counts over the agents a screen is showing
// rather than over the marks beside them.
func (m model) liveAgents() []agentstore.Agent {
	return xslice.Map(m.listing.agents, func(it agentItem) agentstore.Agent { return it.agent })
}

func (m model) agentCount(workflowID string) int {
	return xslice.Count(m.listing.agents, func(it agentItem) bool { return it.agent.WorkflowID == workflowID })
}

type agentItem struct {
	agent      agentstore.Agent
	alive      bool
	unreadMail bool
	unviewed   bool
	failedRun  bool
	// failedRunLog is the run its unread failure was written to, so the pane opens on that one.
	failedRunLog *int32
}

// displayState is the state a row wears, which is idle once the pane has gone even though the
// store still holds whatever the agent stopped in.
func displayState(item agentItem) agentstore.State {
	state := item.agent.State
	if !item.alive && state.DoesPaneDecide() {
		return agentstore.StateIdle
	}

	return state
}

func (m model) liveAgent(id string) (agentItem, bool) {
	for _, item := range m.listing.agents {
		if item.agent.ID == id {
			return item, true
		}
	}

	return agentItem{}, false
}

func (m *model) applyWorkflows(fresh []workflowstore.Workflow) {
	m.listing.workflows = fresh
}

func (m *model) applyAgents(fresh []agentItem) {
	m.listing.agents = fresh
}

// applyMarks takes the letters off both listings at once, since one letter names one row across
// the two and a half-applied pass would draw the same letter twice.
func (m *model) applyMarks(onAgents, onWorkflows map[string]string) {
	marks := make(map[string]string, len(onAgents)+len(onWorkflows))
	maps.Copy(marks, onAgents)
	maps.Copy(marks, onWorkflows)
	m.listing.marks = marks
}

// hoveredAgentKey is the row the cursor is on, read before a filter narrows the list under it.
func (m model) hoveredAgentKey() string {
	return rowKeyAt(m.agentRows(), m.tree.cursor)
}

func (m *model) placeAgentCursor(hovered string) {
	rows := m.agentRows()
	m.tree.cursor = placeCursor(rowIndexOfKey(rows, hovered), m.tree.cursor, len(rows))
	m.stageSelection()
}

func (m *model) placeTreeCursor(selected string) {
	focus := m.tree.focus
	m.tree.focus = ""

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
		clamped := clampCursor(m.tree.cursor, len(rows))
		if strings.HasPrefix(selected, "agent:") {
			clamped = nearestAgentRowIndex(rows, clamped)
		}

		m.tree.cursor = clamped
	}

	m.tree.placed = true
	m.tree.selected = m.tree.selected.pruned(m.allAgentRows())
}

type listing struct {
	agents    []agentItem
	workflows []workflowstore.Workflow
	// marks are the letters a person addresses rows by, keyed by the row key each names, since
	// a mark sits on an agent or on a workflow and the rows are drawn from one tree.
	marks map[string]string
}

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
