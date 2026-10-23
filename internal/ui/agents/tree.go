package agents

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
	agentv1 "komrad/proto/gen/agent/v1"
)

// Deps is what the agents screens are given at launch.
type Deps struct {
	Keys           config.Keybindings
	Glyphs         glyph.Set
	Clock          screen.Clock
	Register       *vim.Register
	Clients        api.Clients
	Panes          screen.Panes
	Top            *keys.Top
	EditorSettings editor.Settings
	TriageRows     int
}

// New is the agents view built from what it is given at launch.
func New(deps Deps) Screen {
	return Screen{
		deps:     deps,
		triage:   triageZone{capacity: deps.TriageRows},
		archived: archivedAgents{deps: deps, kind: agentsArchived},
		deleted:  archivedAgents{deps: deps, kind: agentsDeleted},
	}
}

// Screen is the agents screen: the cursor in the tree of workflows and agents, how they
// are ordered and whether the hidden ones are rendered.
type Screen struct {
	deps Deps
	list.Cursor
	placed     bool
	showHidden bool
	sort       tabs.State
	// rendered is the list built for the frame once an update settles, and nil while one runs.
	rendered *agentsLayout
	tree     screen.Derived[agentTreeKey, []agentRow]
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
	return params.foldOrchestrators && it.Agent.Role == agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR
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
		if row.item.Agent.Role == agentv1.AgentRole_AGENT_ROLE_WRITER {
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
var agentSorts = []tabs.Column[panel.AgentRow]{
	{
		ID:    "created",
		Label: "created",
		Compare: func(a, b panel.AgentRow) int {
			return a.Agent.CreatedAt.Compare(b.Agent.CreatedAt)
		},
	},
	{
		ID:    "activity",
		Label: "activity",
		Compare: func(a, b panel.AgentRow) int {
			return a.Agent.StateChangedAt.Compare(b.Agent.StateChangedAt)
		},
	},
	{
		ID:      "name",
		Label:   "name",
		Compare: byAgentName,
	},
}

func byAgentName(a, b panel.AgentRow) int {
	return strings.Compare(strings.ToLower(a.Agent.Name), strings.ToLower(b.Agent.Name))
}

// sortedAgents is the listing in the order given, the newest agent settling a tie.
func sortedAgents(items []panel.AgentRow, columns []tabs.Column[panel.AgentRow], order tabs.Order) []panel.AgentRow {
	sorted := slices.Clone(items)
	tabs.Sort(tabs.SortParams[panel.AgentRow]{
		Rows:    sorted,
		Columns: columns,
		Order:   order,
		Tiebreak: func(a, b panel.AgentRow) int {
			return b.Agent.CreatedAt.Compare(a.Agent.CreatedAt)
		},
	})

	return sorted
}

// rows are the rows the keyboard can reach.
func (a *Screen) rows(in screen.Inputs) []agentRow {
	return a.layout(in).cursorRows()
}

// layout is the list as rendered, or as it would be rendered when nothing has cached it.
func (a *Screen) layout(in screen.Inputs) agentsLayout {
	if a.rendered != nil {
		return *a.rendered
	}

	return a.buildLayout(in)
}

// BuildFrame lays the list out once for the frame being drawn.
func (a *Screen) BuildFrame(in screen.Inputs) {
	layout := a.buildLayout(in)
	a.rendered = &layout
}

// DropFrame forgets the list built for the last frame, so an update reads the state it changes.
func (a *Screen) DropFrame() {
	a.rendered = nil
}

func (a *Screen) buildLayout(in screen.Inputs) agentsLayout {
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
	order      tabs.Order
	flat       bool
	showHidden bool
	filter     list.PatternKey
}

func (a *Screen) buildTree(in screen.Inputs) []agentRow {
	order := agentsTabs().Order(a.sort)
	key := agentTreeKey{
		generation: in.Listing.Generation,
		order:      order,
		flat:       a.sort.Flat,
		showHidden: a.showHidden,
		filter:     in.Room.Pattern.Key(),
	}

	return a.tree.Read(key, func() []agentRow {
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

func (a *Screen) hoveredRow(in screen.Inputs) (agentRow, bool) {
	return a.layout(in).cursorRow(a.Row)
}

func (a *Screen) hoveredWorkflow(in screen.Inputs) (panel.Workflow, bool) {
	return hoveredHeader(a.hoveredRow(in))
}

// HoveredPane is the agent whose pane the row under the cursor shows, and none where that row
// has no pane worth putting in the window.
func (a *Screen) HoveredPane(in screen.Inputs) (panel.AgentRow, bool) {
	r, ok := a.hoveredRow(in)
	if !ok {
		return panel.AgentRow{}, false
	}

	item, held := r.paneItem()
	if !held || !hasPane(item) || a.isShowingNotes(r) {
		return panel.AgentRow{}, false
	}

	return item, true
}

// Resting is the agent the row under the cursor stands for when it is dormant or paused.
func (a *Screen) Resting(in screen.Inputs) (panel.AgentRow, bool) {
	r, ok := a.hoveredRow(in)
	if !ok {
		return panel.AgentRow{}, false
	}

	item, held := r.paneItem()
	if !held || a.isShowingNotes(r) || item.Alive || item.Agent.State == agentv1.AgentState_AGENT_STATE_SETTING_UP || panel.IsFailed(item.Agent.State) {
		return panel.AgentRow{}, false
	}

	return item, true
}

// RecordAgent is the agent the record under the cursor is about, which for a task is its writer.
func (a *Screen) RecordAgent(in screen.Inputs) (panel.AgentRow, bool) {
	row, ok := a.hoveredRow(in)
	if !ok {
		return panel.AgentRow{}, false
	}

	return row.recordAgent(in.Listing)
}

// RecordTask is the task whose checkout the record under the cursor shows.
func (a *Screen) RecordTask(in screen.Inputs) (panel.Task, bool) {
	row, ok := a.hoveredRow(in)
	if !ok {
		return panel.Task{}, false
	}

	return row.recordTask(in.Listing)
}

// hasNoAgentPane reports whether the row the cursor is on has no pane of its own.
func (a *Screen) hasNoAgentPane(in screen.Inputs) bool {
	_, ok := a.HoveredPane(in)

	return !ok
}

// HoveredAgentID is the agent under the cursor, or the orchestrator behind a hovered workflow.
func (a *Screen) HoveredAgentID(in screen.Inputs) string {
	item, ok := a.hoveredPaneItem(in)
	if !ok {
		return ""
	}

	return item.Agent.ID
}

// hoveredPaneItem is the agent under the cursor, or the orchestrator of the workflow under it.
func (a *Screen) hoveredPaneItem(in screen.Inputs) (panel.AgentRow, bool) {
	row, ok := a.hoveredRow(in)
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

// apply takes a fresh listing, keeping the cursor on the row named.
func (a *Screen) apply(in screen.Inputs, selected string) {
	placed := a.placed
	if !placed {
		selected = ""
	}

	rows := a.rows(in)

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
	a.Selected = a.Selected.Pruned(refsOf(a.rows(in)))
	a.reconcileRename(in)
}

// revealAgent puts the cursor on an agent, showing the hidden rows when it needs them.
func (a *Screen) revealAgent(in screen.Inputs, id string) {
	if toTreeRowKey(a.list().Hovered(in)) == agentRowKey(id) {
		return
	}

	a.showHiddenFor(in.Listing, id)

	rows := a.rows(in)
	if idx := list.IndexOf(rows, findLandingKey(rows, "", agentRowKey(id))); idx >= 0 {
		a.Row = idx
	}
}

// Land puts the cursor on the row a write made, unless a key moved it while the write was parked, and
// closes the editor when the made row brings a pane of its own.
func (a *Screen) Land(in screen.Inputs, made screen.Made, keyMoved bool) screen.Request {
	a.showHiddenFor(in.Listing, made.Agent)

	rows := a.rows(in)
	if key := findLandingKey(rows, a.list().Hovered(in), made.Row); !keyMoved && list.IndexOf(rows, key) >= 0 {
		a.apply(in, key)
	}

	if made.Pane == "" {
		return nil
	}

	return a.dropEdit()
}

// revealTask puts the cursor on a task's row, or on its writer when the list draws no row for it.
func (a *Screen) revealTask(in screen.Inputs, taskID, writerID string) {
	if in.Listing.Tasks[taskID].Hidden {
		a.showHidden = true
	}
	if idx := list.IndexOf(a.rows(in), taskRowKey(taskID)); idx >= 0 {
		a.Row = idx

		return
	}

	a.revealAgent(in, writerID)
}

// showHiddenFor shows the hidden tasks when the agent works on one.
func (a *Screen) showHiddenFor(listing panel.Listing, id string) {
	if listing.Tasks.IsHidden(listing.AgentsByID[id].Agent) {
		a.showHidden = true
	}
}

// agentRowName is the text a search reads a row of the tree by: the workflow's own name on a
// header, the slug on a task, the agent's on a row.
func agentRowName(row agentRow) string {
	if row.isTask() {
		return row.task.Slug
	}
	if row.header {
		return row.workflow.Name
	}

	return row.item.Agent.Name
}

// agentRowDepth is how far a row of the tree is indented: a header, an agent or a task under it,
// or a reviewer under its task.
func agentRowDepth(row agentRow) int {
	switch {
	case row.isTask():
		return 1 + row.depth
	case row.header:
		return 0
	case row.underTask:
		return 2 + row.depth
	}

	return 1
}
