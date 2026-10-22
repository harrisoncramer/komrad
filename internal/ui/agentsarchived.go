package ui

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
	deps agentsDeps
	kind agentsKind
	list.Cursor
	sort  tabs.State
	built derived[archivedRowsKey, []agentRow]
	renameSlot
}

// enterArchived lands the agents view on its archived kind, or its deleted kind when that lists the
// target.
func (m *model) enterArchived(target screen.Target) screen.Request {
	m.agents.kind = agentsArchived
	if isTargetOnDeletedList(m.listing, target) {
		m.agents.kind = agentsDeleted
	}

	return screen.Open{Screen: screen.Agents, Target: target}
}

// enterLive lands the agents view on the live kind.
func (m *model) enterLive(target screen.Target) screen.Request {
	m.agents.kind = agentsLive

	return screen.Open{Screen: screen.Agents, Target: target}
}

func (a *agentsScreen) kinds(*model) tabs.Stops {
	return tabs.Stops{
		Labels: []string{"live", "archived", "deleted"},
		Focus:  tabs.NoStop,
		Lit:    int(a.kind),
	}
}

func (a *agentsScreen) kindsFocused(*model) *bool {
	return &a.kindFocus
}

// kindsSwitch holds the agents view on its kind while the user selects any of its agents.
func (a *agentsScreen) kindsSwitch() bool {
	return !a.Selected.IsActive()
}

// stepKind moves the agents view to its other kind, back on the row it last stood on.
func (a *agentsScreen) stepKind(m *model, _ screen.Inputs, delta int) screen.Request {
	return screen.Leave{Then: func() screen.Request {
		a.kind = agentsKind(tabs.StepStop(int(a.kind), delta, len(a.kinds(m).Labels)))
		var staged screen.Request
		if shown := a.shownArchive(); shown != nil {
			shown.Selected = list.Selection{}
			staged = shown.stage()
		} else {
			a.Selected = list.Selection{}
			staged = a.stage()
		}

		return screen.Batch{screen.ResetFilter{}, screen.Reconcile{}, staged, screen.Settle{}}
	}}
}

// shownArchive is the archived or deleted agents the agents view is on, and nil on the live kind.
func (a *agentsScreen) shownArchive() *archivedAgents {
	switch a.kind {
	case agentsArchived:
		return &a.archived
	case agentsDeleted:
		return &a.deleted
	case agentsLive:
	}

	return nil
}

// enter lands on the task the target names, or on its agent when no row stands for the task.
func (a *archivedAgents) enter(m *model, in screen.Inputs, target screen.Target) screen.Request {
	a.place(in, "")
	rows := a.listRows(in)
	if idx := list.IndexOf(rows, taskRowKey(target.TaskID)); target.TaskID != "" && idx >= 0 {
		a.Row = idx
	} else if idx := rowIndexOfAgentID(rows, target.AgentID); target.AgentID != "" && idx >= 0 {
		a.Row = idx
	}

	return screen.Batch{a.stage(), screen.Settle{}}
}

func (a *archivedAgents) update(m *model, in screen.Inputs, msg tea.Msg) screen.Request {
	if _, ok := msg.(tea.PasteMsg); ok && a.isRenaming() {
		return a.updateRename(a.deps.register, a.deps.clients, msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	a.reconcileRename(in)
	if a.isRenaming() {
		return a.updateRename(a.deps.register, a.deps.clients, msg)
	}

	return a.actions(in).DispatchTop(a.deps.top, a, a.deps.keys, key.String())
}

func (a *archivedAgents) receive(m *model, in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	settled, ok := msg.(api.RenameSettled)
	if !ok {
		return nil, false
	}

	a.settleRename(in, settled.Row, settled.Err)

	return nil, true
}

func (a *archivedAgents) kinds(m *model) tabs.Stops {
	return m.agents.kinds(m)
}

func (a *archivedAgents) kindsFocused(m *model) *bool {
	return m.agents.kindsFocused(m)
}

// kindsSwitch holds the archived agents on their kind while the user selects any of them.
func (a *archivedAgents) kindsSwitch() bool {
	return !a.Selected.IsActive()
}

func (a *archivedAgents) stepKind(m *model, in screen.Inputs, delta int) screen.Request {
	return m.agents.stepKind(m, in, delta)
}

func (a *archivedAgents) list() screenList[agentRow] {
	return screenList[agentRow]{
		keys:   a.deps.keys,
		top:    a.deps.top,
		cursor: func() *list.Cursor { return &a.Cursor },
		rows:   a.listRows,
		name:   agentRowName,
		fits:   agentRowFits,
		refs:   refsOf,
		header: "folder",
		placed: func(screen.Inputs) screen.Request { return a.stage() },
		moved:  func(screen.Inputs) screen.Request { return screen.Batch{a.stage(), screen.Settle{}} },
	}
}

// archivedAgentSorts is every column the archived agents can be ordered by.
var archivedAgentSorts = []sortColumn[panel.AgentRow]{
	{
		id:    "archived",
		label: "archived",
		has: func(item panel.AgentRow) bool {
			return item.Agent.ArchivedAt != nil
		},
		compare: func(a, b panel.AgentRow) int {
			return a.Agent.ArchivedAt.Compare(*b.Agent.ArchivedAt)
		},
	},
	{
		id:      "name",
		label:   "name",
		compare: byAgentName,
	},
}

type archivedRowsKey struct {
	generation int
	order      sortOrder
	flat       bool
	filter     list.PatternKey
}

func (a *archivedAgents) listRows(in screen.Inputs) []agentRow {
	order := stripOrder(archivedAgentTabs(), a.sort)
	key := archivedRowsKey{
		generation: in.Listing.Generation,
		order:      order,
		flat:       a.sort.Flat,
		filter:     in.Room.Pattern.Key(),
	}

	return a.built.read(key, func() []agentRow {
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
	return task.ArchivedAt != nil && task.WorktreeRemovalReason == panel.WorktreeDeleted
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
		if task.WorktreeRemovalReason != panel.WorktreeDeleted {
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

func (a *archivedAgents) isLoading(m *model, in screen.Inputs) bool {
	return slices.ContainsFunc(a.listRows(in), func(r agentRow) bool { return isRowLoading(in.Listing, r) })
}

func (a *archivedAgents) stageResting(m *model, in screen.Inputs) screen.Request {
	return screen.Batch{a.stage(), screen.ReadLastScreen{}, m.inspectTaskWorktree(a.deps.clock.Now())}
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

func (a *archivedAgents) buildItemMenu(in screen.Inputs, nest menu.Nester) (menu.Menu, bool) {
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
			bindings: a.deps.keys,
			item:     item,
		})
	}

	if a.kind == agentsDeleted {
		band.Entries = append(band.Entries, deletedEntries(a.deps.keys, a.deps.clients, target)...)

		return band, true
	}

	return menu.WithNested(band, "archive", a.deps.keys.Menu.Archive, archiveMenu(in, a.deps.keys, a.deps.clients, target)), true
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

	return confirm.open(in, func(form.Inputs) screen.Request {
		return clients.AwaitingWrite(api.AwaitingWriteParams{
			Doing:   "deleting " + workflow.Name,
			Summary: fmt.Sprintf("Could not delete workflow %q", workflow.Name),
			Awaits:  awaitDeleted(confirm.values.deletesRecords(), screen.RowsIn(screen.ListArchivedWorkflows, workflow.ID)),
			Write:   func(a api.Clients) error { return a.DeleteWorkflow(workflow.ID, confirm.values.deletesRecords()) },
		})
	})
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archivedAgents) stage() screen.Request {
	return screen.StageTerminal{}
}

func (a *archivedAgents) listParams(_ *model, in screen.Inputs) list.Params {
	return list.Params{
		Help:     a.help(in),
		Empty:    a.emptySaid(),
		Rows:     len(a.listRows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return a.snapshot(in, pane) },
	}
}

func (a *archivedAgents) help(in screen.Inputs) legend.Help {
	return a.actions(in).Help(a, a.deps.keys)
}

func archivedAgentTabs() tabs.Strip {
	return tabs.Strip{
		Title:    "f",
		Legend:   "sort",
		Options:  tabOptionsOf(archivedAgentSorts),
		Fallback: "archived",
		Grouped:  "workflow",
	}
}

var (
	_ recordRenderer       = (*archivedAgents)(nil)
	_ activeLister         = (*archivedAgents)(nil)
	_ kindSwitcher         = (*archivedAgents)(nil)
	_ tabSwitcher          = (*archivedAgents)(nil)
	_ rowStager            = (*archivedAgents)(nil)
	_ hoveredNamer         = (*archivedAgents)(nil)
	_ itemMenuBuilder      = (*archivedAgents)(nil)
	_ loadingReporter      = (*archivedAgents)(nil)
	_ lineTyper            = (*archivedAgents)(nil)
	_ renamer              = (*archivedAgents)(nil)
	_ hoveredAgentReporter = (*archivedAgents)(nil)
	_ savedScreenRenderer  = (*archivedAgents)(nil)
)

func (a *archivedAgents) doesRenderFromListing() bool {
	return true
}

func (a *archivedAgents) takesEveryKey() bool {
	return a.isRenaming()
}

func (a *archivedAgents) activeList() keyedList {
	return a.list()
}

func (a *archivedAgents) hoveredName(in screen.Inputs) string {
	return a.list().hoveredName(in)
}

func (a *archivedAgents) tabs() tabs.Strip {
	return archivedAgentTabs()
}

func (a *archivedAgents) tabsState() *tabs.State {
	return &a.sort
}

func (a *archivedAgents) column(m *model, in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: columnFloor,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			if agentID, ok := a.screenedAgentID(in); ok {
				if rows := m.said.columnRows(in.Room.Pattern, agentID, width); rows != nil {
					return rows
				}
			}

			return m.withLastScreen(a.docRows(in, width), width)
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
	reconcileRename(&a.renameSlot, a.listRows(in), a.list().hovered(in), agentRowName)
}

func (a *archivedAgents) settleRename(in screen.Inputs, row string, err error) {
	if a.settleField(row, err) {
		a.reconcileRename(in)
	}
}

func (a *archivedAgents) dispatches(*model) []keys.Table {
	return []keys.Table{a.actions(screen.Inputs{})}
}
