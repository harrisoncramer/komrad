package ui

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"komrad/internal/assemble"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
)

// agentsScreen is the agents screen: the cursor in the tree of workflows and agents, how they
// are ordered, whether the hidden ones are drawn, and the mark press waiting on its letter.
type agentsScreen struct {
	rowCursor
	placed     bool
	showHidden bool
	sort       tabsState
	marks      markState
	// drawn is the rows built once for the frame being drawn, and nil between frames.
	drawn []agentRow
}

type agentRow struct {
	header bool
	item   panel.AgentRow
	// hiddenCount is the rows this one stands in for: a workflow header counts the hidden agents
	// under it, and a parent counts its own hidden children.
	hiddenCount int
	workflow    panel.Workflow
	// child says the row is drawn under the one above it rather than under the workflow, which is
	// an indent and nothing else.
	child bool
	// orchestrator is the agent a workflow header stands in for, unset on every other row.
	orchestrator *panel.AgentRow
}

// paneItem is the agent whose pane this row shows, which for a workflow header is the
// orchestrator standing behind it.
func (r agentRow) paneItem() (panel.AgentRow, bool) {
	if r.header {
		if r.orchestrator == nil {
			return panel.AgentRow{}, false
		}

		return *r.orchestrator, true
	}

	return r.item, true
}

type treeRowsParams struct {
	workflows []panel.Workflow
	agents    []panel.AgentRow
	// showHidden draws the agents a person has hidden alongside the rest.
	showHidden bool
	// foldOrchestrators draws a workflow's orchestrator on the workflow's own row rather than
	// as a row of its own.
	foldOrchestrators bool
	// flat draws the families in the order given, with no header over them.
	flat bool
}

// family is one agent and its children as rows, under the workflow the head belongs to.
type family struct {
	workflowID string
	rows       []agentRow
}

// isFolded reports whether an agent is drawn on its workflow's own row rather than on one of its
// own.
func isFolded(params treeRowsParams, it panel.AgentRow) bool {
	return params.foldOrchestrators && it.Agent.Role == panel.RoleOrchestrator
}

func treeRows(params treeRowsParams) []agentRow {
	workflows, agents := params.workflows, params.agents

	children := make(map[string][]panel.AgentRow)
	drawsRow := make(map[string]bool, len(agents))
	orchestrators := make(map[string]panel.AgentRow)
	for _, it := range agents {
		drawsRow[it.Agent.ID] = !isFolded(params, it)
		if it.Agent.ParentAgentID != nil {
			children[*it.Agent.ParentAgentID] = append(children[*it.Agent.ParentAgentID], it)
		}
		if isFolded(params, it) {
			orchestrators[it.Agent.WorkflowID] = it
		}
	}

	var families []family
	hiddenCounts := make(map[string]int)
	for _, it := range agents {
		// An orchestrator is drawn on its workflow's own row, so it stands in no count and no
		// key reveals it here.
		if isFolded(params, it) {
			continue
		}

		// A child is drawn where its parent is, and one whose parent draws no row has nowhere to
		// be indented under, so it stands on its own rather than going missing.
		if it.Agent.ParentAgentID != nil && drawsRow[*it.Agent.ParentAgentID] {
			continue
		}

		workflowID := it.Agent.WorkflowID
		members := append([]panel.AgentRow{it}, children[it.Agent.ID]...)
		if it.Agent.Hidden {
			// Hiding the head takes the rows under it too, so the whole family stands in the
			// workflow's own count.
			hiddenCounts[workflowID] += len(members)
			if !params.showHidden {
				continue
			}
		}

		var rows []agentRow
		for _, member := range members {
			// A hidden child leaves its count on the parent drawn above it rather than on the
			// workflow.
			if member.Agent.Hidden && member.Agent.ID != it.Agent.ID {
				rows[0].hiddenCount++
				if !params.showHidden {
					continue
				}
			}

			rows = append(rows, agentRow{
				item: member,
				// The indent says which row a child hangs off, so one standing at the head of
				// its own family hangs off nothing.
				child: member.Agent.ID != it.Agent.ID,
			})
		}
		families = append(families, family{workflowID: workflowID, rows: rows})
	}

	if params.flat {
		byID := xslice.KeyBy(workflows, func(w panel.Workflow) string { return w.ID })

		var rows []agentRow
		for _, f := range families {
			for _, row := range f.rows {
				row.workflow = byID[f.workflowID]
				rows = append(rows, row)
			}
		}

		return rows
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
		for _, f := range families {
			if f.workflowID != w.ID {
				continue
			}
			for _, row := range f.rows {
				row.workflow = w
				rows = append(rows, row)
			}
		}
	}

	return rows
}

// agentSorts is every column the agents list can be ordered by.
var agentSorts = []sortColumn[panel.AgentRow]{
	{
		id:    "created",
		label: "created",
		compare: func(a, b panel.AgentRow) int {
			return a.Agent.CreatedAt.Compare(b.Agent.CreatedAt)
		},
	},
	{
		id:    "activity",
		label: "activity",
		compare: func(a, b panel.AgentRow) int {
			return a.Agent.StateChangedAt.Compare(b.Agent.StateChangedAt)
		},
	},
	{
		id:      "name",
		label:   "name",
		compare: byAgentName,
	},
}

func byAgentName(a, b panel.AgentRow) int {
	return strings.Compare(strings.ToLower(a.Agent.Name), strings.ToLower(b.Agent.Name))
}

// sortedAgents is the listing in the order given, the newest agent settling a tie.
func sortedAgents(items []panel.AgentRow, columns []sortColumn[panel.AgentRow], order sortOrder) []panel.AgentRow {
	sorted := slices.Clone(items)
	sortRows(sortRowsParams[panel.AgentRow]{
		rows:    sorted,
		columns: columns,
		order:   order,
		tiebreak: func(a, b panel.AgentRow) int {
			return b.Agent.CreatedAt.Compare(a.Agent.CreatedAt)
		},
	})

	return sorted
}

// rows is the tree as drawn, or as it would be drawn when nothing has cached it.
func (a *agentsScreen) rows(s panelShared) []agentRow {
	if a.drawn != nil {
		return a.drawn
	}

	return a.buildRows(s)
}

func (a *agentsScreen) buildRows(s panelShared) []agentRow {
	rows := treeRows(treeRowsParams{
		workflows:         s.listing.workflows,
		agents:            sortedAgents(s.listing.agents, agentSorts, agentsTabs().order(a.sort)),
		showHidden:        a.showHidden,
		foldOrchestrators: true,
		flat:              a.sort.flat,
	})

	return narrowedAgentRows(s.chrome.filter, rows)
}

func narrowedAgentRows(filter filterState, rows []agentRow) []agentRow {
	return narrowed(filter, narrowedParams[agentRow]{
		rows:  rows,
		depth: agentRowDepth,
		name:  agentRowName,
	})
}

func (a *agentsScreen) hoveredRow(s panelShared) (agentRow, bool) {
	return rowAt(a.rows(s), a.cursor)
}

// selectedAgent is the agent the cursor is on, and none on a workflow.
func (a *agentsScreen) selectedAgent(s panelShared) (panel.AgentRow, bool) {
	row, ok := a.hoveredRow(s)
	if !ok || row.header {
		return panel.AgentRow{}, false
	}

	return row.item, true
}

func (a *agentsScreen) hoveredWorkflow(s panelShared) (panel.Workflow, bool) {
	return hoveredHeader(a.rows(s), a.cursor)
}

// hoveredPane is the agent whose pane the row under the cursor shows, and none where that row
// has no pane worth putting in the window.
func (a *agentsScreen) hoveredPane(s panelShared) (panel.AgentRow, bool) {
	r, ok := a.hoveredRow(s)
	if !ok {
		return panel.AgentRow{}, false
	}

	item, held := r.paneItem()
	if !held || !hasPane(item) {
		return panel.AgentRow{}, false
	}

	return item, true
}

// hasNoAgentPane reports whether the row the cursor is on has no pane of its own.
func (a *agentsScreen) hasNoAgentPane(s panelShared) bool {
	_, ok := a.hoveredPane(s)

	return !ok
}

// hoveredAgentID is the agent under the cursor, or the orchestrator behind a hovered workflow.
func (a *agentsScreen) hoveredAgentID(s panelShared) string {
	row, ok := a.hoveredRow(s)
	if !ok {
		return ""
	}

	item, held := row.paneItem()
	if !held {
		return ""
	}

	return item.Agent.ID
}

func hoveredHeader(rows []agentRow, cursor int) (panel.Workflow, bool) {
	row, ok := rowAt(rows, cursor)
	if !ok || !row.header {
		return panel.Workflow{}, false
	}

	return row.workflow, true
}

func (a *agentsScreen) enclosingWorkflow(s panelShared) *panel.Workflow {
	row, ok := a.hoveredRow(s)
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
		if held && item.Agent.ID == id {
			return i
		}
	}

	return -1
}

func rowKey(row agentRow) string {
	if row.header {
		return workflowRowKey(row.workflow.ID)
	}

	return agentRowKey(row.item.Agent.ID)
}

func agentRowKey(id string) string {
	return "agent:" + id
}

// rowID is the id of what a row stands for: the workflow of a header, else the agent.
func rowID(row agentRow) string {
	if row.header {
		return row.workflow.ID
	}

	return row.item.Agent.ID
}

const workflowRowPrefix = "workflow:"

func workflowRowKey(id string) string {
	return workflowRowPrefix + id
}

func (r agentRow) key() string {
	return rowKey(r)
}

// unnamedChildren counts the children the named agents hold that are not among them, taking the
// pool from the screen doing the asking.
func unnamedChildren(pool, named []panel.Agent) int {
	held := make(map[string]bool, len(named))
	for _, agent := range named {
		held[agent.ID] = true
	}

	return xslice.Count(pool, func(agent panel.Agent) bool {
		return agent.ParentAgentID != nil && held[*agent.ParentAgentID] && !held[agent.ID]
	})
}

// liveAgents is the listing as bare rows, for what counts over the agents a screen is showing
// rather than over the marks beside them.
func (l listing) liveAgents() []panel.Agent {
	return xslice.Map(l.agents, func(it panel.AgentRow) panel.Agent { return it.Agent })
}

func (l listing) agentCount(workflowID string) int {
	return xslice.Count(l.agents, func(it panel.AgentRow) bool {
		return it.Agent.WorkflowID == workflowID && it.Agent.Role != panel.RoleOrchestrator
	})
}

// displayState is the state a row wears, idle once the pane has gone even though the store
// still holds whatever the agent stopped in.
func displayState(item panel.AgentRow) panel.State {
	state := item.Agent.State
	if !item.Alive && state.DoesPaneDecide() {
		return panel.StateIdle
	}

	return state
}

func (l listing) liveAgent(id string) (panel.AgentRow, bool) {
	for _, item := range l.agents {
		if item.Agent.ID == id {
			return item, true
		}
	}

	return panel.AgentRow{}, false
}

func (l listing) archivedAgent(id string) (panel.Agent, bool) {
	for _, item := range l.archivedAgents {
		if item.Agent.ID == id {
			return item.Agent, true
		}
	}

	return panel.Agent{}, false
}

// workflowName is what a workflow is called, live or archived, or a dash when none has the id.
func (l listing) workflowName(id string) string {
	for _, w := range slices.Concat(l.workflows, l.archivedWorkflows) {
		if w.ID == id {
			return w.Name
		}
	}

	return label.Dash
}

// apply takes a fresh listing, keeping the cursor on the row named.
func (a *agentsScreen) apply(s panelShared, selected string) {
	placed := a.placed
	if !placed {
		selected = ""
	}

	rows := a.rows(s)

	switch idx := rowIndexOfKey(rows, selected); {
	case idx >= 0:
		a.cursor = idx
	case !placed:
		a.cursor = firstAgentRowIndex(rows)
	default:
		clamped := clampCursor(a.cursor, len(rows))
		if strings.HasPrefix(selected, "agent:") {
			clamped = nearestAgentRowIndex(rows, clamped)
		}

		a.cursor = clamped
	}

	a.placed = true
	a.selected = a.selected.pruned(refsOf(a.rows(s)))
}

// listing is what the screens draw: the resources the watch has delivered, and the live and
// archived rows assembled from them.
type listing struct {
	world             assemble.World
	agents            []panel.AgentRow
	workflows         []panel.Workflow
	archivedAgents    []panel.AgentRow
	archivedWorkflows []panel.Workflow
	schedules         []scheduleItem
	worktrees         []worktreeItem
	// marks are the letters a person types to jump to rows, keyed by the id of the agent or
	// workflow each is on.
	marks map[string]string
}

// assemble draws the listings, live and archived, and the marks from the world as it stands now.
func (l *listing) assemble(now time.Time) {
	agents := agentRowsFrom(assemble.AgentRows(l.world, now))
	l.worktrees = worktreeItemsFrom(l.world.Worktrees, agents)
	isArchivedAgent := func(row panel.AgentRow) bool {
		return row.Agent.ArchivedAt != nil
	}
	l.agents = slices.DeleteFunc(slices.Clone(agents), isArchivedAgent)
	l.archivedAgents = slices.DeleteFunc(agents, func(row panel.AgentRow) bool {
		return !isArchivedAgent(row)
	})

	workflows := workflowsFrom(slices.Collect(maps.Values(l.world.Workflows)))
	slices.SortFunc(workflows, func(a, b panel.Workflow) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(a.ID, b.ID))
	})
	isArchivedWorkflow := func(workflow panel.Workflow) bool {
		return workflow.ArchivedAt != nil
	}
	l.workflows = slices.DeleteFunc(slices.Clone(workflows), isArchivedWorkflow)
	l.archivedWorkflows = slices.DeleteFunc(workflows, func(workflow panel.Workflow) bool {
		return !isArchivedWorkflow(workflow)
	})
	l.schedules = scheduleItemsFrom(slices.Collect(maps.Values(l.world.Schedules)))
	slices.SortFunc(l.schedules, func(a, b scheduleItem) int { return strings.Compare(b.schedule.ID, a.schedule.ID) })
	l.marks = assemble.Marks(l.world)
}

// revealAgent puts the cursor on an agent, showing the hidden rows when it is one of them.
func (a *agentsScreen) revealAgent(s panelShared, id string) {
	for _, item := range s.listing.agents {
		if item.Agent.ID == id && item.Agent.Hidden {
			a.showHidden = true
		}
	}

	if idx := rowIndexOfAgentID(a.rows(s), id); idx >= 0 {
		a.cursor = idx
	}
}
