package ui

import (
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/api"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/editor"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/vim"
	"komrad/lib/xslice"
)

// agentsDeps is what the agents screens are given at launch.
type agentsDeps struct {
	keys           config.Keybindings
	glyphs         glyph.Set
	clock          screen.Clock
	register       *vim.Register
	clients        api.Clients
	panes          screen.Panes
	top            *keys.Top
	editorSettings editor.Settings
}

// agentsScreen is the agents screen: the cursor in the tree of workflows and agents, how they
// are ordered and whether the hidden ones are rendered.
type agentsScreen struct {
	deps agentsDeps
	list.Cursor
	placed     bool
	showHidden bool
	sort       tabs.State
	// rendered is the list built for the frame once an update settles, and nil while one runs.
	rendered *agentsLayout
	tree     derived[agentTreeKey, []agentRow]
	triage   triageZone
	editing  *agentEdit
	// resuming is the agents a resume is in flight for.
	resuming    map[string]struct{}
	awaitingCLI map[string]bool
	renameSlot
	kind      agentsKind
	kindFocus bool
	archived  archivedAgents
	deleted   archivedAgents
	// notes is the task whose notes are shown in place of its writer's pane, empty while none are.
	notes string
}

type agentRow struct {
	header bool
	item   panel.AgentRow
	// hiddenCount is the hidden tasks a workflow header stands in for.
	hiddenCount int
	workflow    panel.Workflow
	// triage says the row is the triage zone's copy of an agent the tree also renders.
	triage bool
	// orchestrator is the agent a workflow header stands in for, unset on every other row.
	orchestrator *panel.AgentRow
	// task makes a header the task's rather than the workflow's, standing for its writer and over its
	// reviewers.
	task *panel.Task
	// underTask sets a reviewer in under its task's row, or under its writer's where no task row is
	// drawn.
	underTask bool
	// depth is how many ancestors a task's row and its reviewers sit under in the tree.
	depth int
}

// isTask reports whether the row is a task's header.
func (r agentRow) isTask() bool {
	return r.header && r.task != nil
}

// isAgentRow reports whether the row stands for agents rather than a workflow: an agent's own row or a
// task's.
func (r agentRow) isAgentRow() bool {
	return !r.header || r.isTask()
}

// writer is the agent a task's row stands for, which a task whose writer is not listed has none of.
func (r agentRow) writer() (panel.AgentRow, bool) {
	return r.item, r.isTask() && r.item.Agent.ID != ""
}

// paneItem is the agent whose pane this row shows, which for a workflow header is the
// orchestrator standing behind it and for a task its writer.
func (r agentRow) paneItem() (panel.AgentRow, bool) {
	if r.isTask() {
		return r.writer()
	}
	if r.header {
		if r.orchestrator == nil {
			return panel.AgentRow{}, false
		}

		return *r.orchestrator, true
	}

	return r.item, true
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
	// flat renders the agents in the order given, with no header over them.
	flat bool
	// tasks are the tasks the agents work on, keyed by id, and nil renders no task headers.
	tasks panel.Tasks
}

func treeRows(params treeRowsParams) []agentRow {
	workflows, agents := params.workflows, params.agents

	writers := panel.WritersByTask(agents)
	reviewers := panel.ReviewersByWriter(agents, writers)
	rendersRow := make(map[string]bool, len(agents))
	orchestrators := make(map[string]panel.AgentRow)
	for _, it := range agents {
		rendersRow[it.Agent.ID] = !isFolded(params, it)
		if isFolded(params, it) {
			orchestrators[it.Agent.WorkflowID] = it
		}
	}

	var listed []agentRow
	hiddenCounts := make(map[string]int)
	countedTasks := make(map[string]bool)
	for _, it := range agents {
		// An orchestrator is rendered on its workflow's own row, so it stands in no count and no
		// key reveals it here.
		if isFolded(params, it) {
			continue
		}

		// A reviewer is rendered where its task's writer is, and one whose writer renders no row has
		// nowhere to be indented under, so it stands on its own rather than going missing.
		if writer, ok := panel.FindWriter(it, writers); ok && rendersRow[writer.Agent.ID] {
			continue
		}

		workflowID := it.Agent.WorkflowID
		if params.tasks.IsHidden(it.Agent) {
			if !countedTasks[it.Agent.TaskID] {
				countedTasks[it.Agent.TaskID] = true
				hiddenCounts[workflowID]++
			}
			if !params.showHidden {
				continue
			}
		}

		listed = append(listed, buildAgentRows(it, reviewers[it.Agent.ID])...)
	}

	if params.flat {
		byID := xslice.KeyBy(workflows, func(w panel.Workflow) string { return w.ID })

		var rows []agentRow
		for _, row := range listed {
			row.workflow = byID[row.item.Agent.WorkflowID]
			rows = append(rows, row)
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
		inWorkflow := xslice.Filter(listed, func(row agentRow) bool { return row.item.Agent.WorkflowID == w.ID })
		for _, row := range taskRows(inWorkflow, params.tasks) {
			row.workflow = w
			rows = append(rows, row)
		}
	}

	return rows
}

// taskRows lays out a workflow's agents, gathering those on one task under a row for it and setting
// a child in under its parent, so only an agent on no task, an orchestrator, sits at the
// agent's step.
func taskRows(listed []agentRow, tasks panel.Tasks) []agentRow {
	if tasks == nil {
		return listed
	}

	onTask := make(map[string][]agentRow)
	for _, row := range listed {
		if taskID := row.item.Agent.TaskID; taskID != "" {
			onTask[taskID] = append(onTask[taskID], row)
		}
	}

	children := make(map[string][]string)
	var order []string
	seen := make(map[string]bool)
	for _, row := range listed {
		taskID := row.item.Agent.TaskID
		if taskID == "" || seen[taskID] {
			continue
		}
		seen[taskID] = true
		order = append(order, taskID)
		if parent := parentListed(tasks, taskID, onTask); parent != "" {
			children[parent] = append(children[parent], taskID)
		}
	}

	var rows []agentRow
	laid := make(map[string]bool)
	var lay func(taskID string, depth int)
	lay = func(taskID string, depth int) {
		if laid[taskID] {
			return
		}
		laid[taskID] = true

		header := taskHeader(onTask[taskID], tasks)
		header.depth = depth
		rows = append(rows, header)
		writer, _ := header.writer()
		for _, reviewer := range onTask[taskID] {
			if reviewer.item.Agent.ID == writer.Agent.ID {
				continue
			}
			reviewer.underTask = true
			reviewer.depth = depth
			rows = append(rows, reviewer)
		}
		for _, child := range children[taskID] {
			lay(child, depth+1)
		}
	}

	for _, row := range listed {
		taskID := row.item.Agent.TaskID
		if taskID == "" {
			rows = append(rows, row)

			continue
		}
		if parentListed(tasks, taskID, onTask) != "" {
			continue
		}
		lay(taskID, 0)
	}

	// A task whose stack loops back on itself has no root to be laid from, so it sits at the top.
	for _, taskID := range order {
		lay(taskID, 0)
	}

	return rows
}

// parentListed is a task's parent when the parent has a row of its own here, empty otherwise.
func parentListed(tasks panel.Tasks, taskID string, onTask map[string][]agentRow) string {
	parent := tasks[taskID].Parent
	if parent == nil {
		return ""
	}
	if _, ok := onTask[parent.GetTaskId()]; !ok {
		return ""
	}

	return parent.GetTaskId()
}

// taskHeader is the row for one task, standing for its writer when the writer is listed.
func taskHeader(gathered []agentRow, tasks panel.Tasks) agentRow {
	task := tasks.FindForAgent(gathered[0].item.Agent)
	header := agentRow{
		header: true,
		task:   &task,
	}
	for _, row := range gathered {
		if row.item.Agent.Role == panel.RoleWriter {
			header.item = row.item
		}
	}

	return header
}

func keyAgentsByID(agents []panel.AgentRow) map[string]panel.AgentRow {
	return xslice.KeyBy(agents, func(item panel.AgentRow) string { return item.Agent.ID })
}

// buildAgentRows is an agent's row with the rows of the reviewers on its task set in under it.
func buildAgentRows(head panel.AgentRow, reviewers []panel.AgentRow) []agentRow {
	return append([]agentRow{{item: head}}, xslice.Map(reviewers, func(reviewer panel.AgentRow) agentRow {
		return agentRow{item: reviewer, underTask: true}
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
func (a *agentsScreen) rows(m *model, in screen.Inputs) []agentRow {
	return a.layout(in).cursorRows()
}

// layout is the list as rendered, or as it would be rendered when nothing has cached it.
func (a *agentsScreen) layout(in screen.Inputs) agentsLayout {
	if a.rendered != nil {
		return *a.rendered
	}

	return a.buildLayout(in)
}

func (a *agentsScreen) buildFrame(_ *model, in screen.Inputs) {
	layout := a.buildLayout(in)
	a.rendered = &layout
}

func (a *agentsScreen) buildLayout(in screen.Inputs) agentsLayout {
	zone, waiting := buildZone(a.triage, in.Listing, a.showHidden)
	tree := a.buildTree(in)

	return agentsLayout{
		zone:    zone,
		waiting: waiting,
		tree:    tree,
		all:     slices.Concat(zone, tree),
	}
}

type agentTreeKey struct {
	generation int
	order      sortOrder
	flat       bool
	showHidden bool
	filter     list.PatternKey
}

func (a *agentsScreen) buildTree(in screen.Inputs) []agentRow {
	order := stripOrder(agentsTabs(), a.sort)
	key := agentTreeKey{
		generation: in.Listing.Generation,
		order:      order,
		flat:       a.sort.Flat,
		showHidden: a.showHidden,
		filter:     in.Room.Pattern.Key(),
	}

	return a.tree.read(key, func() []agentRow {
		return narrowedAgentRows(in, treeRows(treeRowsParams{
			workflows:         in.Listing.Workflows,
			agents:            sortedAgents(in.Listing.Agents, agentSorts, order),
			showHidden:        a.showHidden,
			foldOrchestrators: true,
			flat:              a.sort.Flat,
			tasks:             in.Listing.Tasks,
		}))
	})
}

func narrowedAgentRows(in screen.Inputs, rows []agentRow) []agentRow {
	return list.Narrowed(in.Room.Pattern, list.NarrowedParams[agentRow]{
		Rows:  rows,
		Depth: agentRowDepth,
		Name:  agentRowName,
		Fits:  agentRowFits(in),
	})
}

func (a *agentsScreen) hoveredRow(m *model, in screen.Inputs) (agentRow, bool) {
	return a.layout(in).cursorRow(a.Row)
}

func (a *agentsScreen) hoveredWorkflow(m *model, in screen.Inputs) (panel.Workflow, bool) {
	return hoveredHeader(a.hoveredRow(m, in))
}

// hoveredPane is the agent whose pane the row under the cursor shows, and none where that row
// has no pane worth putting in the window.
func (a *agentsScreen) hoveredPane(m *model, in screen.Inputs) (panel.AgentRow, bool) {
	r, ok := a.hoveredRow(m, in)
	if !ok {
		return panel.AgentRow{}, false
	}

	item, held := r.paneItem()
	if !held || !hasPane(item) || a.isShowingNotes(r) {
		return panel.AgentRow{}, false
	}

	return item, true
}

// hoveredResting is the agent the row under the cursor stands for when it is dormant or paused.
func (a *agentsScreen) hoveredResting(m *model, in screen.Inputs) (panel.AgentRow, bool) {
	r, ok := a.hoveredRow(m, in)
	if !ok {
		return panel.AgentRow{}, false
	}

	item, held := r.paneItem()
	if !held || a.isShowingNotes(r) || (item.Alive && item.Agent.HeldAt == nil) || item.Agent.State == panel.StateSettingUp || item.Agent.State.IsFailed() {
		return panel.AgentRow{}, false
	}

	return item, true
}

// hasNoAgentPane reports whether the row the cursor is on has no pane of its own.
func (a *agentsScreen) hasNoAgentPane(m *model, in screen.Inputs) bool {
	_, ok := a.hoveredPane(m, in)

	return !ok
}

// hoveredAgentID is the agent under the cursor, or the orchestrator behind a hovered workflow.
func (a *agentsScreen) hoveredAgentID(m *model, in screen.Inputs) string {
	item, ok := a.hoveredPaneItem(m, in)
	if !ok {
		return ""
	}

	return item.Agent.ID
}

// hoveredPaneItem is the agent under the cursor, or the orchestrator of the workflow under it.
func (a *agentsScreen) hoveredPaneItem(m *model, in screen.Inputs) (panel.AgentRow, bool) {
	row, ok := a.hoveredRow(m, in)
	if !ok {
		return panel.AgentRow{}, false
	}

	return row.paneItem()
}

func hoveredHeader(row agentRow, ok bool) (panel.Workflow, bool) {
	if !ok || !row.header || row.isTask() {
		return panel.Workflow{}, false
	}

	return row.workflow, true
}

func firstAgentRowIndex(rows []agentRow) int {
	for i, r := range rows {
		if r.isAgentRow() {
			return i
		}
	}

	return 0
}

// nearestAgentRowIndex steers an index off a workflow when the row it used to point at was an
// agent that just vanished.
func nearestAgentRowIndex(rows []agentRow, idx int) int {
	row, ok := list.At(rows, idx)
	if !ok || row.isAgentRow() {
		return idx
	}

	for i := idx + 1; i < len(rows); i++ {
		if rows[i].isAgentRow() {
			return i
		}
	}
	for i := idx - 1; i >= 0; i-- {
		if rows[i].isAgentRow() {
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

// findLandingKey picks which copy of the agent row a landing goes to, preferring the hovered copy,
// then the tree's, then the row of the task a writer stands for, then the zone's.
func findLandingKey(rows []agentRow, hovered, key string) string {
	if toTreeRowKey(hovered) == key {
		return hovered
	}
	if list.IndexOf(rows, key) >= 0 {
		return key
	}
	if at := slices.IndexFunc(rows, func(r agentRow) bool {
		writer, ok := r.writer()

		return ok && agentRowKey(writer.Agent.ID) == key
	}); at >= 0 {
		return rowKey(rows[at])
	}
	if zoneKey := triageRowPrefix + key; list.IndexOf(rows, zoneKey) >= 0 {
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

func (r agentRow) Key() string {
	return rowKey(r)
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

// apply takes a fresh listing, keeping the cursor on the row named.
func (a *agentsScreen) apply(m *model, in screen.Inputs, selected string) {
	placed := a.placed
	if !placed {
		selected = ""
	}

	rows := a.rows(m, in)

	switch idx := list.IndexOf(rows, selected); {
	case idx >= 0:
		a.Row = idx
	case !placed:
		a.Row = firstAgentRowIndex(rows)
	default:
		clamped := cursor.Clamp(a.Row, len(rows))
		if strings.HasPrefix(toTreeRowKey(selected), "agent:") {
			clamped = nearestAgentRowIndex(rows, clamped)
		}

		a.Row = clamped
	}

	a.placed = true
	a.Selected = a.Selected.Pruned(refsOf(a.rows(m, in)))
	a.reconcileRename(m, in)
}

// revealAgent puts the cursor on an agent, showing the hidden rows when it needs them.
func (a *agentsScreen) revealAgent(m *model, in screen.Inputs, id string) {
	if toTreeRowKey(a.list().hovered(m, in)) == agentRowKey(id) {
		return
	}

	a.showHiddenFor(m, id)

	rows := a.rows(m, in)
	if idx := list.IndexOf(rows, findLandingKey(rows, "", agentRowKey(id))); idx >= 0 {
		a.Row = idx
	}
}

// revealTask puts the cursor on a task's row, or on its writer when the list draws no row for it.
func (a *agentsScreen) revealTask(m *model, in screen.Inputs, taskID, writerID string) {
	if in.Listing.Tasks[taskID].Hidden {
		a.showHidden = true
	}
	if idx := list.IndexOf(a.rows(m, in), taskRowKey(taskID)); idx >= 0 {
		a.Row = idx

		return
	}

	a.revealAgent(m, in, writerID)
}

// showHiddenFor shows the hidden tasks when the agent works on one.
func (a *agentsScreen) showHiddenFor(m *model, id string) {
	if m.listing.Tasks.IsHidden(m.listing.AgentsByID[id].Agent) {
		a.showHidden = true
	}
}
