package ui

import (
	"fmt"
	"slices"

	"komrad/internal/ui/api"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
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
	kind agentsKind
	list.Cursor
	sort  tabsState
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

func (a *agentsScreen) kinds(*model) stops {
	return stops{
		labels: []string{"live", "archived", "deleted"},
		focus:  noStop,
		lit:    int(a.kind),
	}
}

func (a *agentsScreen) kindsFocused(*model) *bool {
	return &a.kindFocus
}

func (a *agentsScreen) kindsFit(m *model) bool {
	return m.listFitsTabs(a.listParams(m))
}

// kindsSwitch holds the agents view on its kind while the user selects any of its agents.
func (a *agentsScreen) kindsSwitch(*model) bool {
	return !a.Selected.IsActive()
}

// stepKind moves the agents view to its other kind, back on the row it last stood on.
func (a *agentsScreen) stepKind(m *model, delta int) screen.Request {
	return screen.Leave{Then: func() screen.Request {
		a.kind = agentsKind(stepStop(int(a.kind), delta, len(a.kinds(m).labels)))
		var staged screen.Request
		if shown := a.shownArchive(); shown != nil {
			shown.Selected = list.Selection{}
			staged = shown.stage(m)
		} else {
			a.Selected = list.Selection{}
			staged = a.stage(m)
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
func (a *archivedAgents) enter(m *model, target screen.Target) screen.Request {
	a.place(m, "")
	rows := a.listRows(m)
	if idx := list.IndexOf(rows, taskRowKey(target.TaskID)); target.TaskID != "" && idx >= 0 {
		a.Row = idx
	} else if idx := rowIndexOfAgentID(rows, target.AgentID); target.AgentID != "" && idx >= 0 {
		a.Row = idx
	}

	return screen.Batch{a.stage(m), screen.Settle{}}
}

func (a *archivedAgents) update(m *model, msg tea.Msg) screen.Request {
	if _, ok := msg.(tea.PasteMsg); ok && a.isRenaming() {
		return a.updateRename(m, msg)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	a.reconcileRename(m)
	if a.isRenaming() {
		return a.updateRename(m, msg)
	}

	return a.list().update(m, key, a.actions())
}

func (a *archivedAgents) receive(m *model, msg tea.Msg) (screen.Request, bool) {
	settled, ok := msg.(api.RenameSettled)
	if !ok {
		return nil, false
	}

	a.settleRename(m, settled.Row, settled.Err)

	return nil, true
}

func (a *archivedAgents) kinds(m *model) stops {
	return m.agents.kinds(m)
}

func (a *archivedAgents) kindsFocused(m *model) *bool {
	return m.agents.kindsFocused(m)
}

func (a *archivedAgents) kindsFit(m *model) bool {
	return m.listFitsTabs(a.listParams(m))
}

// kindsSwitch holds the archived agents on their kind while the user selects any of them.
func (a *archivedAgents) kindsSwitch(*model) bool {
	return !a.Selected.IsActive()
}

func (a *archivedAgents) stepKind(m *model, delta int) screen.Request {
	return m.agents.stepKind(m, delta)
}

func (a *archivedAgents) list() screenList[agentRow] {
	return screenList[agentRow]{
		cursor: func() *list.Cursor { return &a.Cursor },
		rows:   a.listRows,
		name:   agentRowName,
		fits:   (*model).agentRowFits,
		refs:   refsOf,
		header: "folder",
		placed: a.stage,
		moved:  func(m *model) screen.Request { return screen.Batch{a.stage(m), screen.Settle{}} },
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
	filter     filterKey
}

func (a *archivedAgents) listRows(m *model) []agentRow {
	order := archivedAgentTabs().order(a.sort)
	key := archivedRowsKey{
		generation: m.listing.Generation,
		order:      order,
		flat:       a.sort.flat,
		filter:     m.filter.key(),
	}

	return a.built.read(key, func() []agentRow {
		return narrowedAgentRows(m, treeRows(treeRowsParams{
			workflows:         a.headers(m.listing),
			agents:            sortedAgents(a.listedAgents(m.listing), archivedAgentSorts, order),
			showHidden:        true,
			foldOrchestrators: !a.sort.flat,
			flat:              a.sort.flat,
			tasks:             m.listing.Tasks,
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
func (a *archivedAgents) place(m *model, hovered string) {
	rows := a.listRows(m)
	a.Selected = a.Selected.Pruned(refsOf(rows))

	a.Row = cursor.Place(list.IndexOf(rows, hovered), a.Row, len(rows))
	a.reconcileRename(m)
}

// hoveredAgent is the archived agent the cursor is on, and none on a folder.
func (a *archivedAgents) hoveredAgent(m *model) (panel.Agent, bool) {
	row, ok := list.At(a.listRows(m), a.Row)
	if !ok || !row.isAgentRow() {
		return panel.Agent{}, false
	}
	item, ok := row.paneItem()

	return item.Agent, ok
}

// hoveredWorkflow is the archived workflow the cursor is on, and none on an agent or on the
// folder of a live workflow.
func (a *archivedAgents) hoveredWorkflow(m *model) (panel.Workflow, bool) {
	workflow, ok := hoveredHeader(list.At(a.listRows(m), a.Row))
	if !ok || workflow.ArchivedAt == nil {
		return panel.Workflow{}, false
	}

	return workflow, true
}

func (a *archivedAgents) hoveredRow(m *model) (agentRow, bool) {
	return list.At(a.listRows(m), a.Row)
}

func (a *archivedAgents) isLoading(m *model) bool {
	return slices.ContainsFunc(a.listRows(m), func(r agentRow) bool { return isRowLoading(m.listing, r) })
}

func (a *archivedAgents) stageResting(m *model) screen.Request {
	return screen.Batch{a.stage(m), screen.ReadLastScreen{}, m.inspectTaskWorktree(m.clock.Now())}
}

// menuTarget is what the archive menu would act on, and none on the folder of a live workflow
// with no archived orchestrator.
func (a *archivedAgents) menuTarget(m *model) (archived, bool) {
	if a.Selected.IsActive() {
		work := splitChosen(m.listing, chosenAgents(a.Selected, a.listRows(m)))

		return archived{
			work:   work,
			title:  describeAgents(work.chosen),
			chosen: true,
		}, len(work.chosen) > 0
	}

	if workflow, ok := a.hoveredWorkflow(m); ok {
		return archived{workflow: &workflow}, true
	}

	row, ok := a.hoveredRow(m)
	if !ok {
		return archived{}, false
	}

	if row.isTask() {
		task := *row.task
		if task.ArchivedAt == nil {
			onTask := xslice.Filter(m.listing.ArchivedAgents, func(item panel.AgentRow) bool { return item.Agent.TaskID == task.ID })
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
		if writer, ok := m.listing.FindTaskWriter(task); ok {
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
		work:    splitChosen(m.listing, []panel.Agent{agent}),
		resumed: &agent,
		title:   m.listing.AgentName(agent),
	}, true
}

func (a *archivedAgents) buildItemMenu(m *model) (menu.Menu, bool) {
	target, ok := a.menuTarget(m)
	if !ok {
		return menu.Menu{}, false
	}

	band := rowMenu(target.name())
	if row, ok := list.At(a.listRows(m), a.Row); ok && a.holdsAnAgent(m) {
		item, _ := row.paneItem()
		band = agentMenu(m, item)
	}

	if a.kind == agentsDeleted {
		band.Entries = append(band.Entries, deletedEntries(m, target)...)

		return band, true
	}

	return menu.WithNested(band, "archive", m.keys.Menu.Archive, archiveMenu(m, target)), true
}

// restoreAndResume takes the agents out of the archive and puts the resumed agent's conversation back in a pane.
func restoreAndResume(m *model, agents []panel.Agent, resumed panel.Agent) screen.Request {
	return openingWrite(m.api, "restoring "+resumed.Name, fmt.Sprintf("Could not restore and open agent %q", resumed.Name), func(a api.Clients) (*agentv1.Agent, error) {
		if err := a.RestoreAgents(panel.IDs(agents)); err != nil {
			return nil, err
		}

		return a.ResumeAgent(resumed.ID)
	})
}

func restoreAgents(m *model, agents []panel.Agent) screen.Request {
	if len(agents) == 0 {
		return nil
	}

	return m.api.AwaitingWrite(
		"restoring "+describeAgents(agents),
		fmt.Sprintf("Could not restore %s", describeAgents(agents)),
		screen.Awaits{Shown: screen.RowsIn(screen.ListAgents, panel.IDs(agents)...)},
		func(a api.Clients) error { return a.RestoreAgents(panel.IDs(agents)) },
	)
}

// confirmDeleteWorkflow asks what goes before it deletes an archived workflow.
func confirmDeleteWorkflow(m *model, workflow panel.Workflow) screen.Request {
	confirm := deleteWorkflowConfirm(workflow, workflow.Worktree.Enabled && !isWorkflowOnDeletedList(m.listing, workflow.ID))

	return confirm.open(m, func() screen.Request {
		return m.api.AwaitingWrite(
			"deleting "+workflow.Name,
			fmt.Sprintf("Could not delete workflow %q", workflow.Name),
			awaitDeleted(confirm.values.deletesRecords(), screen.RowsIn(screen.ListArchivedWorkflows, workflow.ID)),
			func(a api.Clients) error { return a.DeleteWorkflow(workflow.ID, confirm.values.deletesRecords()) },
		)
	})
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archivedAgents) stage(*model) screen.Request {
	return screen.StageTerminal{}
}

func (a *archivedAgents) listParams(m *model) screenListParams {
	return screenListParams{
		help:     a.help,
		empty:    a.emptySaid(),
		rows:     len(a.listRows(m)),
		snapshot: func(pane list.Pane) view.Snapshot { return a.snapshot(m, pane) },
	}
}

func (a *archivedAgents) help(m *model) keyMap {
	return m.helpOf(a.actions())
}

func archivedAgentTabs() tabStrip {
	return tabStrip{
		title:    "f",
		legend:   "sort",
		options:  tabOptionsOf(archivedAgentSorts),
		fallback: "archived",
		grouped:  "workflow",
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

func (a *archivedAgents) detailsFit(m *model) bool {
	return m.listFitsDetails(a.listParams(m))
}

func (a *archivedAgents) detailsRoom(m *model) int {
	return m.listRecordRoom(a.listParams(m))
}

func (a *archivedAgents) activeList() keyedList {
	return a.list()
}

func (a *archivedAgents) hoveredName(m *model) string {
	return a.list().hoveredName(m)
}

func (a *archivedAgents) tabs() tabStrip {
	return archivedAgentTabs()
}

func (a *archivedAgents) tabsState() *tabsState {
	return &a.sort
}

func (a *archivedAgents) tabsFit(m *model) bool {
	return m.listFitsTabs(a.listParams(m))
}

func (a *archivedAgents) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		read:  isAlwaysRead,
		docRows: func(m *model, width int) []string {
			if agentID, ok := a.screenedAgentID(m); ok {
				if rows := m.saidColumnRows(agentID, width); rows != nil {
					return rows
				}
			}

			return m.withLastScreen(a.docRows(m, width), width)
		},
	}
}

// renameable is the row under the cursor when the user can rename it, which is an agent, a task, or a
// live workflow's folder.
func (a *archivedAgents) renameable(m *model) (agentRow, bool) {
	if a.Selected.IsActive() {
		return agentRow{}, false
	}

	row, ok := list.At(a.listRows(m), a.Row)
	if !ok || (row.header && !row.isTask() && row.workflow.ArchivedAt != nil) {
		return agentRow{}, false
	}

	return row, true
}

// openRename types over the name of the agent or live folder under the cursor.
func (a *archivedAgents) openRename(m *model) screen.Request {
	if row, ok := a.renameable(m); ok {
		a.renaming = renameAgentRow(row)
	}

	return nil
}

func (a *archivedAgents) reconcileRename(m *model) {
	reconcileRename(&a.renameSlot, a.listRows(m), a.list().hovered(m), agentRowName)
}

func (a *archivedAgents) settleRename(m *model, row string, err error) {
	if a.settleField(row, err) {
		a.reconcileRename(m)
	}
}

func (a *archivedAgents) dispatches(*model) []keys.Table {
	return []keys.Table{a.actions()}
}
