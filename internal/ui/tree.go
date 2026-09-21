package ui

import (
	"maps"
	"slices"
	"strings"

	"weave/internal/git"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
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
	item   agentItem
	// hiddenCount is the rows this one stands in for: a workflow header counts the hidden agents
	// under it, and a parent counts its own hidden children.
	hiddenCount int
	workflow    panel.Workflow
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
	workflows []panel.Workflow
	agents    []agentItem
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
func isFolded(params treeRowsParams, it agentItem) bool {
	return params.foldOrchestrators && it.agent.Role == panel.RoleOrchestrator
}

func treeRows(params treeRowsParams) []agentRow {
	workflows, agents := params.workflows, params.agents

	children := make(map[string][]agentItem)
	drawsRow := make(map[string]bool, len(agents))
	orchestrators := make(map[string]agentItem)
	for _, it := range agents {
		drawsRow[it.agent.ID] = !isFolded(params, it)
		if it.agent.ParentAgentID != nil {
			children[*it.agent.ParentAgentID] = append(children[*it.agent.ParentAgentID], it)
		}
		if isFolded(params, it) {
			orchestrators[it.agent.WorkflowID] = it
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
		if it.agent.ParentAgentID != nil && drawsRow[*it.agent.ParentAgentID] {
			continue
		}

		workflowID := it.agent.WorkflowID
		members := append([]agentItem{it}, children[it.agent.ID]...)
		if it.agent.Hidden {
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
			if member.agent.Hidden && member.agent.ID != it.agent.ID {
				rows[0].hiddenCount++
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
		id:      "name",
		label:   "name",
		compare: byAgentName,
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
func (a *agentsScreen) selectedAgent(s panelShared) (agentItem, bool) {
	row, ok := a.hoveredRow(s)
	if !ok || row.header {
		return agentItem{}, false
	}

	return row.item, true
}

func (a *agentsScreen) hoveredWorkflow(s panelShared) (panel.Workflow, bool) {
	return hoveredHeader(a.rows(s), a.cursor)
}

// hoveredPane is the agent whose pane the row under the cursor shows, and none where that row
// has no pane worth putting in the window.
func (a *agentsScreen) hoveredPane(s panelShared) (agentItem, bool) {
	r, ok := a.hoveredRow(s)
	if !ok {
		return agentItem{}, false
	}

	item, held := r.paneItem()
	if !held || !hasPane(item) {
		return agentItem{}, false
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

	return item.agent.ID
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
	return xslice.Map(l.agents, func(it agentItem) panel.Agent { return it.agent })
}

func (l listing) agentCount(workflowID string) int {
	return xslice.Count(l.agents, func(it agentItem) bool {
		return it.agent.WorkflowID == workflowID && it.agent.Role != panel.RoleOrchestrator
	})
}

type agentItem struct {
	agent      panel.Agent
	alive      bool
	unreadMail bool
	failedRun  bool
	// failedRunLog is the run its unread failure was written to, so the pane opens on that one.
	failedRunLog *int32
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
func displayState(item agentItem) panel.State {
	state := item.agent.State
	if !item.alive && state.DoesPaneDecide() {
		return panel.StateIdle
	}

	return state
}

func (l listing) liveAgent(id string) (agentItem, bool) {
	for _, item := range l.agents {
		if item.agent.ID == id {
			return item, true
		}
	}

	return agentItem{}, false
}

func (l listing) archivedAgent(id string) (panel.Agent, bool) {
	for _, item := range l.archivedAgents {
		if item.agent.ID == id {
			return item.agent, true
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

func (m *model) applyWorkflows(fresh []panel.Workflow) {
	m.listing.workflows = fresh
}

func (m *model) applyAgents(fresh []agentItem) {
	m.listing.agents = fresh
	m.forgetUnlistedPullRequests()
}

// forgetUnlistedPullRequests drops the pull requests answered for agents neither listing holds.
func (m *model) forgetUnlistedPullRequests() {
	m.pullRequests.forgetUnlisted(slices.Concat(m.listing.agents, m.listing.archivedAgents))
}

// applyMarks takes the letters off both listings at once.
func (m *model) applyMarks(onAgents, onWorkflows map[string]string) {
	marks := make(map[string]string, len(onAgents)+len(onWorkflows))
	maps.Copy(marks, onAgents)
	maps.Copy(marks, onWorkflows)
	m.listing.marks = marks
}

// apply takes a fresh listing, keeping the cursor on the row it was on, or on the row a write
// asked it to land on.
func (a *agentsScreen) apply(s panelShared, selected string) {
	focus := a.focus
	a.focus = ""

	placed := a.placed
	if !placed {
		selected = ""
	}

	rows := a.rows(s)

	idx := rowIndexOfKey(rows, selected)
	if focus != "" {
		idx = rowIndexOfKey(rows, focus)
	}

	switch {
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

// focusOn remembers the row the next listing lands the cursor on.
func (a *agentsScreen) focusOn(row string) {
	a.focus = row
}

type listing struct {
	agents            []agentItem
	workflows         []panel.Workflow
	archivedAgents    []agentItem
	archivedWorkflows []panel.Workflow
	schedules         []scheduleItem
	// marks are the letters a person types to jump to rows, keyed by the row key each names.
	marks map[string]string
}

// revealAgent puts the cursor on an agent, showing the hidden rows when it is one of them.
func (a *agentsScreen) revealAgent(s panelShared, id string) {
	for _, item := range s.listing.agents {
		if item.agent.ID == id && item.agent.Hidden {
			a.showHidden = true
		}
	}

	if idx := rowIndexOfAgentID(a.rows(s), id); idx >= 0 {
		a.cursor = idx
	}
}
