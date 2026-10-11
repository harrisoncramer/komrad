package ui

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"komrad/internal/assemble"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
)

// agentsScreen is the agents screen: the cursor in the tree of workflows and agents, how they
// are ordered and whether the hidden ones are rendered.
type agentsScreen struct {
	rowCursor
	placed     bool
	showHidden bool
	sort       tabsState
	// rendered is the list built for the frame once an update settles, and nil while one runs.
	rendered *agentsLayout
	triage   triageZone
	editing  *agentEdit
	// resuming is the agents a resume is in flight for.
	resuming    map[string]struct{}
	awaitingCLI map[string]bool
	renameSlot
	kind      agentsKind
	kindFocus bool
	archived  archivedAgents
}

type agentRow struct {
	header bool
	item   panel.AgentRow
	// hiddenCount is the hidden tasks a workflow header stands in for.
	hiddenCount int
	workflow    panel.Workflow
	// child says the row is rendered under the one above it rather than under the workflow, which is
	// an indent and nothing else.
	child bool
	// triage says the row is the triage zone's copy of an agent the tree also renders.
	triage bool
	// orchestrator is the agent a workflow header stands in for, unset on every other row.
	orchestrator *panel.AgentRow
	// task makes a header the task's rather than the workflow's, standing over the agents on it.
	task *panel.Task
	// lead is the agent a task's header wears the state and pull request of: the newest with a
	// pane, or the newest when none has one.
	lead *panel.AgentRow
	// underTask sets the row in under the task header above it.
	underTask bool
}

// isTask reports whether the row is a task's header.
func (r agentRow) isTask() bool {
	return r.header && r.task != nil
}

// paneItem is the agent whose pane this row shows, which for a workflow header is the
// orchestrator standing behind it and for a task is none.
func (r agentRow) paneItem() (panel.AgentRow, bool) {
	if r.isTask() {
		return panel.AgentRow{}, false
	}
	if r.header {
		if r.orchestrator == nil {
			return panel.AgentRow{}, false
		}

		return *r.orchestrator, true
	}

	return r.item, true
}

// family is one agent and its children as rows, under the workflow the head belongs to.
type family struct {
	workflowID string
	taskID     string
	rows       []agentRow
}

// isFolded reports whether an agent is rendered on its workflow's own row rather than on one of its
// own.
func isFolded(params treeRowsParams, it panel.AgentRow) bool {
	return params.foldOrchestrators && it.Agent.Role == panel.RoleOrchestrator
}

type treeRowsParams struct {
	workflows []panel.Workflow
	agents    []panel.AgentRow
	// showHidden renders the tasks a person has hidden alongside the rest.
	showHidden bool
	// foldOrchestrators renders a workflow's orchestrator on the workflow's own row rather than
	// as a row of its own.
	foldOrchestrators bool
	// flat renders the families in the order given, with no header over them.
	flat bool
	// tasks are the tasks the agents work on, keyed by id, and nil renders no task headers.
	tasks map[string]panel.Task
}

func treeRows(params treeRowsParams) []agentRow {
	workflows, agents := params.workflows, params.agents

	children := childrenByParent(agents)
	rendersRow := make(map[string]bool, len(agents))
	orchestrators := make(map[string]panel.AgentRow)
	for _, it := range agents {
		rendersRow[it.Agent.ID] = !isFolded(params, it)
		if isFolded(params, it) {
			orchestrators[it.Agent.WorkflowID] = it
		}
	}

	var families []family
	hiddenCounts := make(map[string]int)
	countedTasks := make(map[string]bool)
	for _, it := range agents {
		// An orchestrator is rendered on its workflow's own row, so it stands in no count and no
		// key reveals it here.
		if isFolded(params, it) {
			continue
		}

		// A child is rendered where its parent is, and one whose parent renders no row has nowhere to
		// be indented under, so it stands on its own rather than going missing.
		if it.Agent.ParentAgentID != nil && rendersRow[*it.Agent.ParentAgentID] {
			continue
		}

		workflowID := it.Agent.WorkflowID
		if isOnHiddenTask(params.tasks, it.Agent) {
			if !countedTasks[it.Agent.TaskID] {
				countedTasks[it.Agent.TaskID] = true
				hiddenCounts[workflowID]++
			}
			if !params.showHidden {
				continue
			}
		}

		families = append(families, family{
			workflowID: workflowID,
			taskID:     it.Agent.TaskID,
			rows:       buildFamilyRows(it, children[it.Agent.ID]),
		})
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
		inWorkflow := xslice.Filter(families, func(f family) bool { return f.workflowID == w.ID })
		for _, row := range taskRows(inWorkflow, params.tasks) {
			row.workflow = w
			rows = append(rows, row)
		}
	}

	return rows
}

// taskRows lays out a workflow's families, gathering those on one task under a header for it,
// so only an agent on no task, an orchestrator, sits at the agent's step.
func taskRows(families []family, tasks map[string]panel.Task) []agentRow {
	if tasks == nil {
		return slices.Concat(xslice.Map(families, func(f family) []agentRow { return f.rows })...)
	}

	onTask := make(map[string][]family)
	for _, f := range families {
		if f.taskID != "" {
			onTask[f.taskID] = append(onTask[f.taskID], f)
		}
	}

	var rows []agentRow
	laid := make(map[string]bool)
	for _, f := range families {
		gathered := onTask[f.taskID]
		if len(gathered) == 0 {
			rows = append(rows, f.rows...)

			continue
		}
		if laid[f.taskID] {
			continue
		}
		laid[f.taskID] = true

		rows = append(rows, taskHeader(gathered, tasks))
		for _, g := range gathered {
			for _, row := range g.rows {
				row.underTask = true
				rows = append(rows, row)
			}
		}
	}

	return rows
}

// taskHeader is the row over the families on one task, led by the newest of their heads with a
// pane, or the newest of them when none has one.
func taskHeader(gathered []family, tasks map[string]panel.Task) agentRow {
	lead := gathered[0].rows[0].item
	for _, f := range gathered[1:] {
		if head := f.rows[0].item; isBetterLead(head, lead) {
			lead = head
		}
	}

	task := findAgentTask(tasks, lead.Agent)

	return agentRow{header: true, task: &task, lead: &lead}
}

// isBetterLead reports whether a task's header should wear the head over the lead it has so far.
func isBetterLead(head, lead panel.AgentRow) bool {
	if hasPane(head) != hasPane(lead) {
		return hasPane(head)
	}

	return head.Agent.CreatedAt.After(lead.Agent.CreatedAt)
}

func keyAgentsByID(agents []panel.AgentRow) map[string]panel.AgentRow {
	return xslice.KeyBy(agents, func(item panel.AgentRow) string { return item.Agent.ID })
}

func childrenByParent(agents []panel.AgentRow) map[string][]panel.AgentRow {
	children := make(map[string][]panel.AgentRow)
	for _, item := range agents {
		if parentID := item.Agent.ParentAgentID; parentID != nil {
			children[*parentID] = append(children[*parentID], item)
		}
	}

	return children
}

func buildFamilyRows(head panel.AgentRow, children []panel.AgentRow) []agentRow {
	return append([]agentRow{{item: head}}, xslice.Map(children, func(child panel.AgentRow) agentRow {
		return agentRow{item: child, child: true}
	})...)
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

// rows are the rows the keyboard can reach.
func (a *agentsScreen) rows(m *model) []agentRow {
	return a.layout(m).cursorRows()
}

// layout is the list as rendered, or as it would be rendered when nothing has cached it.
func (a *agentsScreen) layout(m *model) agentsLayout {
	if a.rendered != nil {
		return *a.rendered
	}

	return a.buildLayout(m)
}

func (a *agentsScreen) buildFrame(m *model) {
	layout := a.buildLayout(m)
	a.rendered = &layout
}

func (a *agentsScreen) buildLayout(m *model) agentsLayout {
	zone, waiting := buildZone(a.triage, m, a.showHidden)
	tree := treeRows(treeRowsParams{
		workflows:         m.listing.workflows,
		agents:            sortedAgents(m.listing.agents, agentSorts, agentsTabs().order(a.sort)),
		showHidden:        a.showHidden,
		foldOrchestrators: true,
		flat:              a.sort.flat,
		tasks:             m.listing.tasks,
	})

	return agentsLayout{
		zone:    zone,
		waiting: waiting,
		tree:    narrowedAgentRows(m, tree),
	}
}

func narrowedAgentRows(m *model, rows []agentRow) []agentRow {
	return narrowed(m.filter, narrowedParams[agentRow]{
		rows:  rows,
		depth: agentRowDepth,
		name:  agentRowName,
		fits:  m.agentRowFits,
	})
}

func (a *agentsScreen) hoveredRow(m *model) (agentRow, bool) {
	return rowAt(a.rows(m), a.cursor)
}

func (a *agentsScreen) hoveredWorkflow(m *model) (panel.Workflow, bool) {
	return hoveredHeader(a.rows(m), a.cursor)
}

// hoveredPane is the agent whose pane the row under the cursor shows, and none where that row
// has no pane worth putting in the window.
func (a *agentsScreen) hoveredPane(m *model) (panel.AgentRow, bool) {
	r, ok := a.hoveredRow(m)
	if !ok {
		return panel.AgentRow{}, false
	}

	item, held := r.paneItem()
	if !held || !hasPane(item) {
		return panel.AgentRow{}, false
	}

	return item, true
}

// hoveredResting is the agent the row under the cursor stands for when it is dormant or paused.
func (a *agentsScreen) hoveredResting(m *model) (panel.AgentRow, bool) {
	r, ok := a.hoveredRow(m)
	if !ok {
		return panel.AgentRow{}, false
	}

	item, held := r.paneItem()
	if !held || (item.Alive && item.Agent.HeldAt == nil) || item.Agent.State == panel.StateSettingUp || item.Agent.State.IsFailed() {
		return panel.AgentRow{}, false
	}

	return item, true
}

// hasNoAgentPane reports whether the row the cursor is on has no pane of its own.
func (a *agentsScreen) hasNoAgentPane(m *model) bool {
	_, ok := a.hoveredPane(m)

	return !ok
}

// hoveredAgentID is the agent under the cursor, or the orchestrator behind a hovered workflow.
func (a *agentsScreen) hoveredAgentID(m *model) string {
	item, ok := a.hoveredPaneItem(m)
	if !ok {
		return ""
	}

	return item.Agent.ID
}

// hoveredPaneItem is the agent under the cursor, or the orchestrator of the workflow under it.
func (a *agentsScreen) hoveredPaneItem(m *model) (panel.AgentRow, bool) {
	row, ok := a.hoveredRow(m)
	if !ok {
		return panel.AgentRow{}, false
	}

	return row.paneItem()
}

func hoveredHeader(rows []agentRow, cursor int) (panel.Workflow, bool) {
	row, ok := rowAt(rows, cursor)
	if !ok || !row.header || row.isTask() {
		return panel.Workflow{}, false
	}

	return row.workflow, true
}

func (a *agentsScreen) enclosingWorkflow(m *model) *panel.Workflow {
	row, ok := a.hoveredRow(m)
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
		if held && !r.isTask() && item.Agent.ID == id {
			return i
		}
	}

	return -1
}

func rowKey(row agentRow) string {
	if row.isTask() {
		return taskRowKey(row.task.ID)
	}
	if row.header {
		return workflowRowKey(row.workflow.ID)
	}
	if row.triage {
		return triageRowPrefix + agentRowKey(row.item.Agent.ID)
	}

	return agentRowKey(row.item.Agent.ID)
}

func agentRowKey(id string) string {
	return "agent:" + id
}

const triageRowPrefix = "triage:"

// toTreeRowKey is the key of the tree's row for the row the key names.
func toTreeRowKey(key string) string {
	return strings.TrimPrefix(key, triageRowPrefix)
}

// findLandingKey picks which copy of the agent row a landing goes to, preferring the hovered copy, then the tree's, then the zone's.
func findLandingKey(rows []agentRow, hovered, key string) string {
	if toTreeRowKey(hovered) == key {
		return hovered
	}
	if rowIndexOfKey(rows, key) >= 0 {
		return key
	}
	if zoneKey := triageRowPrefix + key; rowIndexOfKey(rows, zoneKey) >= 0 {
		return zoneKey
	}

	return key
}

const workflowRowPrefix = "workflow:"

const taskRowPrefix = "task:"

func workflowRowKey(id string) string {
	return workflowRowPrefix + id
}

func taskRowKey(id string) string {
	return taskRowPrefix + id
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
	if !item.Alive && state.DoesPaneDecide() && state != panel.StateErrored {
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

// anyAgent is the agent with the id, live or archived.
func (l listing) anyAgent(id string) (panel.AgentRow, bool) {
	if item, ok := l.liveAgent(id); ok {
		return item, true
	}

	for _, item := range l.archivedAgents {
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

// agentName is what an agent is called in the panel, its workflow's name for an orchestrator.
func (l listing) agentName(agent panel.Agent) string {
	if agent.Role == panel.RoleOrchestrator {
		return l.workflowName(agent.WorkflowID)
	}

	return agent.Name
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
func (a *agentsScreen) apply(m *model, selected string) {
	placed := a.placed
	if !placed {
		selected = ""
	}

	rows := a.rows(m)

	switch idx := rowIndexOfKey(rows, selected); {
	case idx >= 0:
		a.cursor = idx
	case !placed:
		a.cursor = firstAgentRowIndex(rows)
	default:
		clamped := cursor.Clamp(a.cursor, len(rows))
		if strings.HasPrefix(toTreeRowKey(selected), "agent:") {
			clamped = nearestAgentRowIndex(rows, clamped)
		}

		a.cursor = clamped
	}

	a.placed = true
	a.selected = a.selected.pruned(refsOf(a.rows(m)))
	a.reconcileRename(m)
}

// listing is what the screens render: the resources the watch has delivered, and the live and
// archived rows assembled from them.
type listing struct {
	world             assemble.World
	agents            []panel.AgentRow
	workflows         []panel.Workflow
	archivedAgents    []panel.AgentRow
	archivedWorkflows []panel.Workflow
	tasks             map[string]panel.Task
	schedules         []scheduleItem
	archivedSchedules []scheduleItem
	worktrees         []worktreeItem
	documentTypes     []*agentv1.DocumentType
}

// assemble renders the listings, live and archived, from the world as it stands now.
func (l *listing) assemble(now time.Time) {
	agents := agentRowsFrom(assemble.AgentRows(l.world, now), l.world.Tasks)
	l.tasks = tasksFrom(assemble.TaskRows(l.world))
	l.worktrees = worktreeItemsFrom(l.world.Worktrees, agents, l.tasks)
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
	schedules := scheduleItemsFrom(slices.Collect(maps.Values(l.world.Schedules)))
	slices.SortFunc(schedules, func(a, b scheduleItem) int { return strings.Compare(b.schedule.ID, a.schedule.ID) })
	isArchivedSchedule := func(item scheduleItem) bool {
		return item.schedule.ArchivedAt != nil
	}
	l.schedules = slices.DeleteFunc(slices.Clone(schedules), isArchivedSchedule)
	l.archivedSchedules = slices.DeleteFunc(schedules, func(item scheduleItem) bool {
		return !isArchivedSchedule(item)
	})
	l.documentTypes = documentTypesFrom(l.world.DocumentTypes)
}

// revealAgent puts the cursor on an agent, showing the hidden rows when it needs them.
func (a *agentsScreen) revealAgent(m *model, id string) {
	if toTreeRowKey(a.list().hovered(m)) == agentRowKey(id) {
		return
	}

	a.showHiddenFor(m, id)

	rows := a.rows(m)
	if idx := rowIndexOfKey(rows, findLandingKey(rows, "", agentRowKey(id))); idx >= 0 {
		a.cursor = idx
	}
}

// showHiddenFor shows the hidden tasks when the agent works on one.
func (a *agentsScreen) showHiddenFor(m *model, id string) {
	if isOnHiddenTask(m.listing.tasks, keyAgentsByID(m.listing.agents)[id].Agent) {
		a.showHidden = true
	}
}
