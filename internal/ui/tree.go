package ui

import (
	"slices"
	"strings"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"
)

type treeState struct {
	rowCursor

	placed bool

	showHidden bool
}

type agentRow struct {
	header bool
	item   agentItem

	workflow workflowstore.Workflow

	hiddenCount int
}

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

func (m model) agentRows() []agentRow {
	return m.filter.filterAgentRows(m.allAgentRows())
}

func (m model) allAgentRows() []agentRow {
	return treeRows(m.treeHeaders(), m.listing.agents, m.tree.showHidden)
}

// treeHeaders is the live workflows, plus any archived workflow that still holds a live
// agent. Archiving a workflow archives what is inside it, so an archived header here means
// a row was left behind, and a row whose header is missing is drawn nowhere at all.
func (m model) treeHeaders() []workflowstore.Workflow {
	holding := xslice.KeyBy(m.listing.agents, func(item agentItem) string { return item.agent.WorkflowID })

	headers := slices.Clone(m.listing.workflows)
	for _, w := range m.listing.archivedWorkflows {
		if _, isHolding := holding[w.ID]; isHolding {
			headers = append(headers, w)
		}
	}

	return headers
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

// hoveredWorkflow is the workflow the workflow keys act on, which an archived header is not.
// That header is drawn only to carry a row left behind in it, and everything those keys do —
// starting an agent, renaming, editing, archiving again — is refused for an archived workflow.
func (m model) hoveredWorkflow() (workflowstore.Workflow, bool) {
	workflow, ok := hoveredHeader(m.agentRows(), m.tree.cursor)
	if !ok || workflow.ArchivedAt != nil {
		return workflowstore.Workflow{}, false
	}

	return workflow, true
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
	if !ok || row.workflow.ArchivedAt != nil {
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

func (m model) agentCount(workflowID string) int {
	return xslice.Count(m.listing.agents, func(it agentItem) bool { return it.agent.WorkflowID == workflowID })
}

type agentItem struct {
	agent      agentstore.Agent
	alive      bool
	unreadMail bool
}

func (m *model) applyWorkflows(fresh []workflowstore.Workflow) {
	m.listing.workflows = fresh
}

func (m *model) applyArchivedWorkflows(fresh []workflowstore.Workflow) {
	m.listing.archivedWorkflows = fresh
}

func (m *model) applyAgents(fresh []agentItem) {
	m.listing.agents = fresh
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
	agents            []agentItem
	workflows         []workflowstore.Workflow
	archivedWorkflows []workflowstore.Workflow
}

func (m *model) revealAgent(id string) bool {
	for _, item := range m.listing.agents {
		if item.agent.ID == id && item.agent.Hidden {
			m.tree.showHidden = true
		}
	}

	idx := rowIndexOfAgentID(m.agentRows(), id)
	if idx < 0 {
		return false
	}

	m.tree.cursor = idx

	return true
}
