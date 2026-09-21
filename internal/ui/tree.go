package ui

import (
	"maps"
	"slices"
	"strings"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
)

type treeState struct {
	rowCursor
	placed     bool
	showHidden bool
	sort       tabsState
	rows       []agentRow
}

type agentRow struct {
	header bool
	item   agentItem
	// hiddenCount is the rows this one stands in for: a workflow header counts the hidden agents
	// under it, and a parent counts its own hidden children.
	hiddenCount int
	workflow    workflowstore.Workflow
	// child says the row is drawn under the one above it rather than under the workflow, which is
	// an indent and nothing else.
	child bool
	// orchestrator is the agent a workflow header stands in for, unset on every other row.
	orchestrator *agentItem
}

// paneItem is the agent whose pane this row shows, which for a workflow header is the
// orchestrator standing behind it.
func (r agentRow) paneItem() (agentItem, bool) {
	if r.header {
		if r.orchestrator == nil {
			return agentItem{}, false
		}

		return *r.orchestrator, true
	}

	return r.item, true
}

type treeRowsParams struct {
	workflows []workflowstore.Workflow
	agents    []agentItem
	// showHidden draws the agents a person has hidden alongside the rest.
	showHidden bool
	// foldOrchestrators draws a workflow's orchestrator on the workflow's own row rather than
	// as a row of its own.
	foldOrchestrators bool
}

// folded reports whether an agent is drawn on its workflow's own row rather than on one of its
// own.
func folded(params treeRowsParams, it agentItem) bool {
	return params.foldOrchestrators && it.agent.Role == agentstore.RoleOrchestrator
}

func treeRows(params treeRowsParams) []agentRow {
	workflows, agents := params.workflows, params.agents

	children := make(map[string][]agentItem)
	drawsRow := make(map[string]bool, len(agents))
	orchestrators := make(map[string]agentItem)
	for _, it := range agents {
		drawsRow[it.agent.ID] = !folded(params, it)
		if it.agent.ParentAgentID != nil {
			children[*it.agent.ParentAgentID] = append(children[*it.agent.ParentAgentID], it)
		}
		if folded(params, it) {
			orchestrators[it.agent.WorkflowID] = it
		}
	}

	byWorkflow := make(map[string][]agentRow)
	hiddenCounts := make(map[string]int)
	for _, it := range agents {
		// An orchestrator is drawn on its workflow's own row, so it stands in no count and no
		// key reveals it here.
		if folded(params, it) {
			continue
		}

		// A child is drawn where its parent is, and one whose parent draws no row has nowhere to
		// be indented under, so it stands on its own rather than going missing.
		if it.agent.ParentAgentID != nil && drawsRow[*it.agent.ParentAgentID] {
			continue
		}

		workflowID := it.agent.WorkflowID
		family := append([]agentItem{it}, children[it.agent.ID]...)
		if it.agent.Hidden {
			// Hiding the head takes the rows under it too, so the whole family stands in the
			// workflow's own count.
			hiddenCounts[workflowID] += len(family)
			if !params.showHidden {
				continue
			}
		}

		rows := byWorkflow[workflowID]
		head := len(rows)
		for _, member := range family {
			// A hidden child leaves its count on the parent drawn above it rather than on the
			// workflow.
			if member.agent.Hidden && member.agent.ID != it.agent.ID {
				rows[head].hiddenCount++
				if !params.showHidden {
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
		header := agentRow{
			header:      true,
			workflow:    w,
			hiddenCount: hiddenCounts[w.ID],
		}
		if found, ok := orchestrators[w.ID]; ok {
			header.orchestrator = &found
		}

		rows = append(rows, header)
		for _, row := range byWorkflow[w.ID] {
			row.workflow = w
			rows = append(rows, row)
		}
	}

	return rows
}

// agentSorts is every column the agents list can be ordered by.
var agentSorts = []sortColumn[agentItem]{
	{
		id:    "created",
		label: "created",
		compare: func(a, b agentItem) int {
			return a.agent.CreatedAt.Compare(b.agent.CreatedAt)
		},
	},
	{
		id:    "activity",
		label: "activity",
		compare: func(a, b agentItem) int {
			return a.agent.StateChangedAt.Compare(b.agent.StateChangedAt)
		},
	},
	{
		id:        "name",
		label:     "name",
		direction: tabUp,
		compare:   byAgentName,
	},
}

func byAgentName(a, b agentItem) int {
	return strings.Compare(strings.ToLower(a.agent.Name), strings.ToLower(b.agent.Name))
}

// sortedAgents is the listing in the order given, the newest agent settling a tie.
func sortedAgents(items []agentItem, columns []sortColumn[agentItem], order sortOrder) []agentItem {
	sorted := slices.Clone(items)
	sortRows(sortRowsParams[agentItem]{
		rows:    sorted,
		columns: columns,
		order:   order,
		tiebreak: func(a, b agentItem) int {
			return b.agent.CreatedAt.Compare(a.agent.CreatedAt)
		},
	})

	return sorted
}

func (m model) agentRows() []agentRow {
	if m.tree.rows != nil {
		return m.tree.rows
	}

	return m.buildAgentRows()
}

func (m model) buildAgentRows() []agentRow {
	rows := treeRows(treeRowsParams{
		workflows:         m.listing.workflows,
		agents:            sortedAgents(m.listing.agents, agentSorts, agentsTabs().order(m.tree.sort)),
		showHidden:        m.tree.showHidden,
		foldOrchestrators: true,
	})

	return narrowedAgentRows(m.filter, rows)
}

func narrowedAgentRows(filter filterState, rows []agentRow) []agentRow {
	return narrowed(filter, narrowedParams[agentRow]{
		rows:  rows,
		depth: agentRowDepth,
		name:  agentRowName,
	})
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

// nearestAgentRowIndex steers an index off a header when the row it used to point at was an
// agent that just vanished.
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
		item, held := r.paneItem()
		if held && item.agent.ID == id {
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

func (r agentRow) key() string {
	return rowKey(r)
}

// unnamedChildren counts the children the named agents hold that are not among them, taking the
// pool from the screen doing the asking.
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
	return xslice.Count(m.listing.agents, func(it agentItem) bool {
		return it.agent.WorkflowID == workflowID && it.agent.Role != agentstore.RoleOrchestrator
	})
}

type agentItem struct {
	agent      agentstore.Agent
	alive      bool
	unreadMail bool
	failedRun  bool
	// failedRunLog is the run its unread failure was written to, so the pane opens on that one.
	failedRunLog *int32
	// localRounds is how many rounds of local review this agent's own work has been through.
	localRounds int32
	// localVerdict is what the last released local review concluded about this agent's work,
	// UNSPECIFIED when none has concluded anything.
	localVerdict agentv1.ReviewVerdict
	// localTree is where the worktree stands against the round that verdict was given on.
	localTree git.Standing
	// pullRequest is where the pull request opened from its branch stands.
	pullRequest pullRequestStanding
}

// displayState is the state a row wears, idle once the pane has gone even though the store
// still holds whatever the agent stopped in.
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
	m.forgetUnlistedPullRequests(fresh)
}

// applyMarks takes the letters off both listings at once.
func (m *model) applyMarks(onAgents, onWorkflows map[string]string) {
	marks := make(map[string]string, len(onAgents)+len(onWorkflows))
	maps.Copy(marks, onAgents)
	maps.Copy(marks, onWorkflows)
	m.listing.marks = marks
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
	m.tree.selected = m.tree.selected.pruned(refsOf(m.agentRows()))
}

type listing struct {
	agents    []agentItem
	workflows []workflowstore.Workflow
	// marks are the letters a person types to jump to rows, keyed by the row key each names.
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
