package agents

import (
	"fmt"
	"slices"

	"komrad/internal/ui/api"
	"komrad/internal/ui/column"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"

	tea "charm.land/bubbletea/v2"
)

// agentsKind is which agents the agents view is listing.
type agentsKind int

const (
	agentsLive agentsKind = iota
	agentsArchived
	agentsDeleted
)

// archivedAgents is the agents view on its archived or deleted kind, holding the cursor in the agents
// that kind lists and how they sort.
type archivedAgents struct {
	deps Deps
	kind agentsKind
	list.Cursor
	sort  tabs.State
	built screen.Derived[archivedRowsKey, []agentRow]
	renameSlot
}

// Kinds is the agents view's row of kinds, live, archived and deleted.
func (a *Screen) Kinds() tabs.Stops {
	return tabs.Stops{
		Labels: []string{"live", "archived", "deleted"},
		Focus:  tabs.NoStop,
		Lit:    int(a.kind),
	}
}

// KindsFocused is whether the row of kinds holds the keys.
func (a *Screen) KindsFocused() *bool {
	return &a.kindFocus
}

// KindsSwitch holds the agents view on its kind while the user selects any of its agents.
func (a *Screen) KindsSwitch() bool {
	return !a.Selected.IsActive()
}

// StepKind moves the agents view to its other kind, back on the row it last stood on.
func (a *Screen) StepKind(_ screen.Inputs, delta int) screen.Request {
	return screen.Leave{Then: func() screen.Request {
		a.kind = agentsKind(tabs.StepStop(int(a.kind), delta, len(a.Kinds().Labels)))
		var staged screen.Request
		if shown := a.archiveOfKind(); shown != nil {
			shown.Selected = list.Selection{}
			staged = shown.Stage()
		} else {
			a.Selected = list.Selection{}
			staged = a.Stage()
		}

		return screen.Batch{screen.ResetFilter{}, screen.Reconcile{}, staged, screen.Settle{}}
	}}
}

// archiveOfKind is the archived or deleted agents the agents view is on, and nil on the live kind.
func (a *Screen) archiveOfKind() *archivedAgents {
	switch a.kind {
	case agentsArchived:
		return &a.archived
	case agentsDeleted:
		return &a.deleted
	case agentsLive:
	}

	return nil
}

// ShownArchive is the archived or deleted agents the agents view is on, as the screen that renders
// them, and false on the live kind.
func (a *Screen) ShownArchive() (Archive, bool) {
	shown := a.archiveOfKind()
	if shown == nil {
		return Archive{}, false
	}

	return a.archiveScreen(shown), true
}

// ShowLive puts the agents view on its live kind.
func (a *Screen) ShowLive() {
	a.kind = agentsLive
}

// ShowArchived puts the agents view on its archived kind, or its deleted kind when that lists the
// target.
func (a *Screen) ShowArchived(l panel.Listing, target screen.Target) {
	a.kind = agentsArchived
	if isTargetOnDeletedList(l, target) {
		a.kind = agentsDeleted
	}
}

// Archived is the view's archived kind as the screen that renders it.
func (a *Screen) Archived() Archive {
	return a.archiveScreen(&a.archived)
}

// Deleted is the view's deleted kind as the screen that renders it.
func (a *Screen) Deleted() Archive {
	return a.archiveScreen(&a.deleted)
}

// Archive is an archive of the agents view, its own state paired with the view whose
// kinds row it shares.
type Archive struct {
	*archivedAgents
	agents *Screen
}

// archiveScreen is one of the view's archives as the screen that renders it.
func (a *Screen) archiveScreen(archive *archivedAgents) Archive {
	return Archive{archivedAgents: archive, agents: a}
}

// Kinds is the agents view's row of kinds.
func (a Archive) Kinds() tabs.Stops {
	return a.agents.Kinds()
}

// KindsFocused is whether the row of kinds holds the keys.
func (a Archive) KindsFocused() *bool {
	return a.agents.KindsFocused()
}

// StepKind moves the agents view to another kind.
func (a Archive) StepKind(in screen.Inputs, delta int) screen.Request {
	return a.agents.StepKind(in, delta)
}

// Enter lands on the task the target names, or on its agent when no row stands for the task.
func (a *archivedAgents) Enter(in screen.Inputs, target screen.Target) screen.Request {
	a.place(in, "")
	rows := a.listRows(in)
	if idx := list.IndexOf(rows, taskRowKey(target.TaskID)); target.TaskID != "" && idx >= 0 {
		a.Row = idx
	} else if idx := rowIndexOfAgentID(rows, target.AgentID); target.AgentID != "" && idx >= 0 {
		a.Row = idx
	}

	return screen.Batch{a.Stage(), screen.Settle{}}
}

// Update sends a key to the rename being typed, or to the archive's table.
func (a Archive) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	if _, ok := msg.(tea.PasteMsg); ok && a.IsRenaming() {
		return a.updateRename(a.deps.Register, a.deps.Clients, msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	a.reconcileRename(in)
	if a.IsRenaming() {
		return a.updateRename(a.deps.Register, a.deps.Clients, msg)
	}

	return a.actions(in).DispatchTop(a.deps.Top, a, a.deps.Keys, key.String())
}

func (a *archivedAgents) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	settled, ok := msg.(api.RenameSettled)
	if !ok {
		return nil, false
	}

	a.settleRename(in, settled.Row, settled.Err)

	return nil, true
}

// KindsSwitch holds the archived agents on their kind while the user selects any of them.
func (a *archivedAgents) KindsSwitch() bool {
	return !a.Selected.IsActive()
}

func (a *archivedAgents) list() keys.ScreenList[agentRow] {
	return keys.ScreenList[agentRow]{
		Keys:   a.deps.Keys,
		Cursor: func() *list.Cursor { return &a.Cursor },
		Rows:   a.listRows,
		Name:   agentRowName,
		Fits:   agentRowFits,
		Refs:   refsOf,
		Header: "folder",
		Placed: func(screen.Inputs) screen.Request { return a.Stage() },
		Moved:  func(screen.Inputs) screen.Request { return screen.Batch{a.Stage(), screen.Settle{}} },
	}
}

// archivedAgentSorts is every column the archived agents can be ordered by.
var archivedAgentSorts = []tabs.Column[panel.AgentRow]{
	{
		ID:    "archived",
		Label: "archived",
		Has: func(item panel.AgentRow) bool {
			return item.Agent.ArchivedAt != nil
		},
		Compare: func(a, b panel.AgentRow) int {
			return a.Agent.ArchivedAt.Compare(*b.Agent.ArchivedAt)
		},
	},
	{
		ID:      "name",
		Label:   "name",
		Compare: byAgentName,
	},
}

type archivedRowsKey struct {
	generation int
	order      tabs.Order
	flat       bool
	filter     list.PatternKey
}

func (a *archivedAgents) listRows(in screen.Inputs) []agentRow {
	order := archivedAgentTabs().Order(a.sort)
	key := archivedRowsKey{
		generation: in.Listing.Generation,
		order:      order,
		flat:       a.sort.Flat,
		filter:     in.Room.Pattern.Key(),
	}

	return a.built.Read(key, func() []agentRow {
		return narrowedAgentRows(in, treeRows(treeRowsParams{
			workflows:         a.headers(in.Listing),
			agents:            sortedAgents(a.listedAgents(in.Listing), archivedAgentSorts, order),
			showHidden:        true,
			foldOrchestrators: !a.sort.Flat,
			flat:              a.sort.Flat,
			tasks:             in.Listing.Tasks,
		}))
	})
}

// listedAgents is the archived agents this kind lists: those on the deleted list, or every other one.
func (a *archivedAgents) listedAgents(l panel.Listing) []panel.AgentRow {
	return xslice.Filter(l.ArchivedAgents, func(item panel.AgentRow) bool {
		return isAgentOnDeletedList(l, item) == (a.kind == agentsDeleted)
	})
}

// headers is every workflow this kind renders a folder for, live or archived.
func (a *archivedAgents) headers(l panel.Listing) []panel.Workflow {
	holding := xslice.KeyBy(a.listedAgents(l), func(item panel.AgentRow) string { return item.Agent.WorkflowID })
	archived := xslice.KeyBy(l.ArchivedWorkflows, func(w panel.Workflow) string { return w.ID })

	headers := xslice.Filter(l.ArchivedWorkflows, func(w panel.Workflow) bool {
		if a.kind != agentsDeleted {
			return !isWorkflowOnDeletedList(l, w.ID)
		}
		_, isHolding := holding[w.ID]

		return isHolding || isWorkflowOnDeletedList(l, w.ID)
	})
	for _, w := range l.Workflows {
		_, isHolding := holding[w.ID]
		_, isArchived := archived[w.ID]
		if isHolding && !isArchived {
			headers = append(headers, w)
		}
	}

	// The merge takes from two listings, so the result is sorted again the way the query sorts.
	slices.SortStableFunc(headers, panel.ByName)

	return headers
}

// isAgentOnDeletedList reports whether an archived agent shows on the deleted list: one on an
// archived task whose worktree was deleted, or the orchestrator of an archived workflow whose every
// task's was.
func isAgentOnDeletedList(l panel.Listing, item panel.AgentRow) bool {
	if item.Agent.TaskID == "" {
		return isWorkflowOnDeletedList(l, item.Agent.WorkflowID)
	}

	return isTaskOnDeletedList(l.Tasks.FindForAgent(item.Agent))
}

// isTargetOnDeletedList reports whether the deleted list holds the task or the archived agent a
// target names.
func isTargetOnDeletedList(l panel.Listing, target screen.Target) bool {
	if task, listed := l.Tasks[target.TaskID]; listed {
		return isTaskOnDeletedList(task)
	}

	return slices.ContainsFunc(l.ArchivedAgents, func(item panel.AgentRow) bool {
		return item.Agent.ID == target.AgentID && isAgentOnDeletedList(l, item)
	})
}

func isTaskOnDeletedList(task panel.Task) bool {
	return task.ArchivedAt != nil && task.WorktreeRemovalReason == taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED
}

// isWorkflowOnDeletedList reports whether an archived workflow has a task and the worktree of
// every one was deleted.
func isWorkflowOnDeletedList(l panel.Listing, workflowID string) bool {
	if !slices.ContainsFunc(l.ArchivedWorkflows, func(w panel.Workflow) bool { return w.ID == workflowID }) {
		return false
	}

	hasTask := false
	for _, task := range l.Tasks {
		if task.WorkflowID != workflowID {
			continue
		}
		if task.WorktreeRemovalReason != taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED {
			return false
		}
		hasTask = true
	}

	return hasTask
}

// place keeps the cursor on the row it was on as the rows under it change.
func (a *archivedAgents) place(in screen.Inputs, hovered string) {
	rows := a.listRows(in)
	a.Selected = a.Selected.Pruned(refsOf(rows))

	a.Row = cursor.Place(list.IndexOf(rows, hovered), a.Row, len(rows))
	a.reconcileRename(in)
}

// hoveredAgent is the archived agent the cursor is on, and none on a folder.
func (a *archivedAgents) hoveredAgent(in screen.Inputs) (panel.Agent, bool) {
	row, ok := list.At(a.listRows(in), a.Row)
	if !ok || !row.isAgentRow() {
		return panel.Agent{}, false
	}
	item, ok := row.paneItem()

	return item.Agent, ok
}

// hoveredWorkflow is the archived workflow the cursor is on, and none on an agent or on the
// folder of a live workflow.
func (a *archivedAgents) hoveredWorkflow(in screen.Inputs) (panel.Workflow, bool) {
	workflow, ok := hoveredHeader(list.At(a.listRows(in), a.Row))
	if !ok || workflow.ArchivedAt == nil {
		return panel.Workflow{}, false
	}

	return workflow, true
}

func (a *archivedAgents) hoveredRow(in screen.Inputs) (agentRow, bool) {
	return list.At(a.listRows(in), a.Row)
}

func (a *archivedAgents) IsLoading(in screen.Inputs) bool {
	return slices.ContainsFunc(a.listRows(in), func(r agentRow) bool { return isRowLoading(in.Listing, r) })
}

func (a *archivedAgents) StageResting(screen.Inputs) screen.Request {
	return screen.Batch{a.Stage(), screen.ReadLastScreen{}}
}

// menuTarget is what the archive menu would act on, and none on the folder of a live workflow
// with no archived orchestrator.
func (a *archivedAgents) menuTarget(in screen.Inputs) (archived, bool) {
	if a.Selected.IsActive() {
		work := splitChosen(in.Listing, chosenAgents(a.Selected, a.listRows(in)))

		return archived{
			work:   work,
			title:  describeAgents(work.chosen),
			chosen: true,
		}, len(work.chosen) > 0
	}

	if workflow, ok := a.hoveredWorkflow(in); ok {
		return archived{workflow: &workflow}, true
	}

	row, ok := a.hoveredRow(in)
	if !ok {
		return archived{}, false
	}

	if row.isTask() {
		task := *row.task
		if task.ArchivedAt == nil {
			onTask := xslice.Filter(in.Listing.ArchivedAgents, func(item panel.AgentRow) bool { return item.Agent.TaskID == task.ID })
			reviewers := xslice.Map(onTask, func(item panel.AgentRow) panel.Agent { return item.Agent })

			return archived{
				work:  chosenWork{chosen: reviewers, loneAgents: reviewers},
				title: task.Slug,
			}, true
		}

		target := archived{
			work:  chosenWork{wholeTasks: []panel.Task{task}},
			title: task.Slug,
		}
		if writer, ok := in.Listing.FindTaskWriter(task); ok {
			target.work.chosen = []panel.Agent{writer.Agent}
			target.resumed = &writer.Agent
		}

		return target, true
	}

	item, ok := row.paneItem()
	if !ok {
		return archived{}, false
	}

	agent := item.Agent

	return archived{
		work:    splitChosen(in.Listing, []panel.Agent{agent}),
		resumed: &agent,
		title:   in.Listing.AgentName(agent),
	}, true
}

func (a *archivedAgents) BuildItemMenu(in screen.Inputs, nest menu.Nester) (menu.Menu, bool) {
	target, ok := a.menuTarget(in)
	if !ok {
		return menu.Menu{}, false
	}

	band := rowMenu(target.name())
	if row, ok := list.At(a.listRows(in), a.Row); ok && a.holdsAnAgent(in) {
		item, _ := row.paneItem()
		band = agentMenu(agentMenuParams{
			in:       in,
			nest:     nest,
			bindings: a.deps.Keys,
			item:     item,
		})
	}

	if a.kind == agentsDeleted {
		band.Entries = append(band.Entries, deletedEntries(a.deps.Keys, a.deps.Clients, target)...)

		return band, true
	}

	return menu.WithNested(band, "archive", a.deps.Keys.Menu.Archive, archiveMenu(in, a.deps.Keys, a.deps.Clients, target)), true
}

// restoreAndResume takes the agents out of the archive and puts the resumed agent's conversation back in a pane.
func restoreAndResume(clients api.Clients, agents []panel.Agent, resumed panel.Agent) screen.Request {
	return openingWrite(clients, "restoring "+resumed.Name, fmt.Sprintf("Could not restore and open agent %q", resumed.Name), func(a api.Clients) (*agentv1.Agent, error) {
		if err := a.RestoreAgents(panel.IDs(agents)); err != nil {
			return nil, err
		}

		return a.ResumeAgent(resumed.ID)
	})
}

func restoreAgents(clients api.Clients, agents []panel.Agent) screen.Request {
	if len(agents) == 0 {
		return nil
	}

	return clients.AwaitingWrite(api.AwaitingWriteParams{
		Doing:   "restoring " + describeAgents(agents),
		Summary: fmt.Sprintf("Could not restore %s", describeAgents(agents)),
		Awaits:  screen.Awaits{Shown: screen.RowsIn(screen.ListAgents, panel.IDs(agents)...)},
		Write:   func(a api.Clients) error { return a.RestoreAgents(panel.IDs(agents)) },
	})
}

// confirmDeleteWorkflow asks what goes before it deletes an archived workflow.
func confirmDeleteWorkflow(in screen.Inputs, clients api.Clients, workflow panel.Workflow) screen.Request {
	confirm := deleteWorkflowConfirm(workflow, workflow.Worktree.Enabled && !isWorkflowOnDeletedList(in.Listing, workflow.ID))

	return confirm.Open(in, func(form.Inputs) screen.Request {
		return clients.AwaitingWrite(api.AwaitingWriteParams{
			Doing:   "deleting " + workflow.Name,
			Summary: fmt.Sprintf("Could not delete workflow %q", workflow.Name),
			Awaits:  awaitDeleted(confirm.Values.DeletesRecords(), screen.RowsIn(screen.ListArchivedWorkflows, workflow.ID)),
			Write:   func(a api.Clients) error { return a.DeleteWorkflow(workflow.ID, confirm.Values.DeletesRecords()) },
		})
	})
}

// Stage keeps the info pane on the row the cursor is standing on.
func (a *archivedAgents) Stage() screen.Request {
	return screen.StageTerminal{}
}

// ListParams is the list of archived or deleted agents as the root renders it.
func (a Archive) ListParams(in screen.Inputs) list.Params {
	return list.Params{
		Help:     a.help(in),
		Empty:    a.emptySaid(),
		Rows:     len(a.listRows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return a.snapshot(in, pane) },
	}
}

func (a Archive) help(in screen.Inputs) legend.Help {
	return a.actions(in).Help(a, a.deps.Keys)
}

func archivedAgentTabs() tabs.Strip {
	return tabs.Strip{
		Title:    "f",
		Legend:   "sort",
		Options:  tabs.OptionsOf(archivedAgentSorts),
		Fallback: "archived",
		Grouped:  "workflow",
	}
}

func (a *archivedAgents) DoesRenderFromListing() bool {
	return true
}

func (a *archivedAgents) TakesEveryKey() bool {
	return a.IsRenaming()
}

func (a *archivedAgents) ActiveList() keys.KeyedList {
	return a.list()
}

func (a *archivedAgents) HoveredName(in screen.Inputs) string {
	return a.list().HoveredName(in)
}

func (a *archivedAgents) Tabs() tabs.Strip {
	return archivedAgentTabs()
}

func (a *archivedAgents) TabsState() *tabs.State {
	return &a.sort
}

func (a *archivedAgents) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return a.docRows(in, width)
		},
		Said: func() string {
			agentID, _ := a.ScreenedAgentID(in)

			return agentID
		},
	}
}

// renameable is the row under the cursor when the user can rename it, which is an agent, a task, or a
// live workflow's folder.
func (a *archivedAgents) renameable(in screen.Inputs) (agentRow, bool) {
	if a.Selected.IsActive() {
		return agentRow{}, false
	}

	row, ok := list.At(a.listRows(in), a.Row)
	if !ok || (row.header && !row.isTask() && row.workflow.ArchivedAt != nil) {
		return agentRow{}, false
	}

	return row, true
}

// openRename types over the name of the agent or live folder under the cursor.
func (a *archivedAgents) openRename(in screen.Inputs) screen.Request {
	if row, ok := a.renameable(in); ok {
		a.renaming = renameAgentRow(row)
	}

	return nil
}

func (a *archivedAgents) reconcileRename(in screen.Inputs) {
	reconcileRename(&a.renameSlot, a.listRows(in), a.list().Hovered(in), agentRowName)
}

func (a *archivedAgents) settleRename(in screen.Inputs, row string, err error) {
	if a.settleField(row, err) {
		a.reconcileRename(in)
	}
}

// Dispatches is the archive's table.
func (a Archive) Dispatches() []keys.Table {
	return []keys.Table{a.actions(screen.Inputs{})}
}
