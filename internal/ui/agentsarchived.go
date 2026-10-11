package ui

import (
	"fmt"
	"slices"
	"time"

	"komrad/internal/ui/cursor"
	"komrad/internal/ui/panel"
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
)

// archivedAgents is the agents view on its archived kind: the cursor in the archived agents and
// how they are ordered.
type archivedAgents struct {
	rowCursor
	sort  tabsState
	built derived[archivedRowsKey, []agentRow]
	renameSlot
}

// enterArchived lands the agents view on its archived kind.
func (m *model) enterArchived(target screenTarget) tea.Cmd {
	m.agents.kind = agentsArchived

	return m.enter(screenAgents, target)
}

// enterLive lands the agents view on the live kind.
func (m *model) enterLive(target screenTarget) tea.Cmd {
	m.agents.kind = agentsLive

	return m.enter(screenAgents, target)
}

func (a *agentsScreen) kinds(*model) stops {
	return stops{
		labels: []string{"live", "archived"},
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

// kindsSwitch holds the agents view on its kind while any of its agents are selected.
func (a *agentsScreen) kindsSwitch(*model) bool {
	return !a.selected.isActive()
}

// stepKind moves the agents view to its other kind with the cursor on the first row.
func (a *agentsScreen) stepKind(m *model, delta int) tea.Cmd {
	left := m.leaveScreen()
	m.filter = filterState{}
	a.kind = agentsKind(stepStop(int(a.kind), delta, len(a.kinds(m).labels)))
	m.reconcileSolo()
	if a.kind == agentsArchived {
		a.archived.selected = selection{}
		a.archived.cursor = 0
		a.archived.stage(m)
	} else {
		a.selected = selection{}
		a.cursor = 0
		m.stageSelection()
	}

	return tea.Batch(left, m.waitToStage())
}

// enter lands on the task the target names, or on its agent when no row stands for the task.
func (a *archivedAgents) enter(m *model, target screenTarget) tea.Cmd {
	a.place(m, "")
	rows := a.listRows(m)
	if idx := rowIndexOfKey(rows, taskRowKey(target.taskID)); target.taskID != "" && idx >= 0 {
		a.cursor = idx
	} else if idx := rowIndexOfAgentID(rows, target.agentID); target.agentID != "" && idx >= 0 {
		a.cursor = idx
	}

	a.stage(m)

	return m.waitToStage()
}

func (a *archivedAgents) update(m *model, msg tea.Msg) tea.Cmd {
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

func (a *archivedAgents) kinds(m *model) stops {
	return m.agents.kinds(m)
}

func (a *archivedAgents) kindsFocused(m *model) *bool {
	return m.agents.kindsFocused(m)
}

func (a *archivedAgents) kindsFit(m *model) bool {
	return m.listFitsTabs(a.listParams(m))
}

// kindsSwitch holds the archived agents on their kind while any of them are selected.
func (a *archivedAgents) kindsSwitch(*model) bool {
	return !a.selected.isActive()
}

func (a *archivedAgents) stepKind(m *model, delta int) tea.Cmd {
	return m.agents.stepKind(m, delta)
}

func (a *archivedAgents) list() screenList[agentRow] {
	return screenList[agentRow]{
		cursor: func() *rowCursor { return &a.rowCursor },
		rows:   a.listRows,
		name:   agentRowName,
		fits:   (*model).agentRowFits,
		refs:   refsOf,
		header: "folder",
		placed: a.stage,
		moved: func(m *model) tea.Cmd {
			a.stage(m)

			return m.waitToStage()
		},
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
		generation: m.listing.generation,
		order:      order,
		flat:       a.sort.flat,
		filter:     m.filter.key(),
	}

	return a.built.read(key, func() []agentRow {
		return narrowedAgentRows(m, treeRows(treeRowsParams{
			workflows:         archivedAgentHeaders(m.listing),
			agents:            sortedAgents(m.listing.archivedAgents, archivedAgentSorts, order),
			showHidden:        true,
			foldOrchestrators: !a.sort.flat,
			flat:              a.sort.flat,
			tasks:             m.listing.tasks,
		}))
	})
}

// archivedAgentHeaders is every workflow the archived agents render a folder for: the archived ones, and the
// live ones still holding an archived agent.
func archivedAgentHeaders(l listing) []panel.Workflow {
	holding := xslice.KeyBy(l.archivedAgents, func(item panel.AgentRow) string { return item.Agent.WorkflowID })
	archived := xslice.KeyBy(l.archivedWorkflows, func(w panel.Workflow) string { return w.ID })

	headers := slices.Clone(l.archivedWorkflows)
	for _, w := range l.workflows {
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

// archivedAgentsOf is the archive listing as bare rows.
func archivedAgentsOf(l listing) []panel.Agent {
	return xslice.Map(l.archivedAgents, func(item panel.AgentRow) panel.Agent { return item.Agent })
}

func (l listing) archivedCount(workflowID string) int {
	return xslice.Count(l.archivedAgents, func(item panel.AgentRow) bool { return item.Agent.WorkflowID == workflowID })
}

// place keeps the cursor on the row it was on as the rows under it change.
func (a *archivedAgents) place(m *model, hovered string) {
	rows := a.listRows(m)
	a.selected = a.selected.pruned(refsOf(rows))

	a.cursor = cursor.Place(rowIndexOfKey(rows, hovered), a.cursor, len(rows))
	a.reconcileRename(m)
}

// hoveredAgent is the archived agent the cursor is on, and none on a folder.
func (a *archivedAgents) hoveredAgent(m *model) (panel.Agent, bool) {
	row, ok := rowAt(a.listRows(m), a.cursor)
	if !ok || row.header {
		return panel.Agent{}, false
	}

	return row.item.Agent, true
}

// hoveredWorkflow is the archived workflow the cursor is on, and none on an agent or on the
// folder of a live workflow.
func (a *archivedAgents) hoveredWorkflow(m *model) (panel.Workflow, bool) {
	workflow, ok := hoveredHeader(rowAt(a.listRows(m), a.cursor))
	if !ok || workflow.ArchivedAt == nil {
		return panel.Workflow{}, false
	}

	return workflow, true
}

func (a *archivedAgents) hoveredRow(m *model) (agentRow, bool) {
	return rowAt(a.listRows(m), a.cursor)
}

func (a *archivedAgents) isLoading(m *model) bool {
	return slices.ContainsFunc(a.listRows(m), m.listing.isRowLoading)
}

func (a *archivedAgents) stageResting(m *model) tea.Cmd {
	a.stage(m)

	return tea.Batch(m.readLastScreenCmd(), m.inspectTaskWorktreeCmd(time.Now()))
}

// menuTarget is what the archive menu would act on, and none on the folder of a live workflow
// with no archived orchestrator.
func (a *archivedAgents) menuTarget(m *model) (archived, bool) {
	if a.selected.isActive() {
		work := splitChosen(m.listing, chosenAgents(a.selected, a.listRows(m)))

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
		onTask := xslice.Filter(m.listing.archivedAgents, func(item panel.AgentRow) bool { return item.Agent.TaskID == task.ID })
		agents := xslice.Map(onTask, func(item panel.AgentRow) panel.Agent { return item.Agent })
		target := archived{
			work:  chosenWork{chosen: agents, loneAgents: agents},
			title: task.Slug,
		}
		if lead, ok := findLead(onTask); ok {
			target.lead = &lead.Agent
		}
		if task.ArchivedAt != nil {
			writers := xslice.Filter(agents, func(agent panel.Agent) bool { return agent.Role == panel.RoleWriter })
			target.work = chosenWork{chosen: writers, wholeTasks: []panel.Task{task}}
			if len(writers) > 0 {
				target.lead = &writers[0]
			}
		}

		return target, true
	}

	item, ok := row.paneItem()
	if !ok {
		return archived{}, false
	}

	agent := item.Agent

	return archived{
		work:  splitChosen(m.listing, []panel.Agent{agent}),
		lead:  &agent,
		title: m.listing.agentName(agent),
	}, true
}

func (a *archivedAgents) buildItemMenu(m *model) (submenu, bool) {
	target, ok := a.menuTarget(m)
	if !ok {
		return submenu{}, false
	}

	archive := archiveMenu(m, target)

	if row, ok := rowAt(a.listRows(m), a.cursor); ok && a.holdsAnAgent(m) && !row.header {
		return withNested(agentMenu(m, row.item), "archive", m.keys.Menu.Archive, archive), true
	}

	return withNested(rowMenu(target.name()), "archive", m.keys.Menu.Archive, archive), true
}

// restoreAndResumeCmd takes the agents out of the archive and puts the lead's conversation back in a pane.
func restoreAndResumeCmd(m *model, agents []panel.Agent, lead panel.Agent) tea.Cmd {
	return m.openingWriteCmd("restoring "+lead.Name, fmt.Sprintf("Could not restore and open agent %q", lead.Name), func(a api) (*agentv1.Agent, error) {
		if err := a.restoreAgents(panel.IDs(agents)); err != nil {
			return nil, err
		}

		return a.resumeAgent(lead.ID)
	})
}

func restoreAgentsCmd(m *model, agents []panel.Agent) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}

	return m.awaitingWriteCmd(
		"restoring "+describeAgents(agents),
		fmt.Sprintf("Could not restore %s", describeAgents(agents)),
		&awaits{shown: rowsIn(listAgents, panel.IDs(agents)...)},
		func(a api) error { return a.restoreAgents(panel.IDs(agents)) },
	)
}

// confirmDeleteWorkflow asks before an archived workflow is deleted for good.
func confirmDeleteWorkflow(m *model, workflow panel.Workflow) tea.Cmd {
	form, confirmed := deleteWorkflowForm(workflow, m.listing.archivedCount(workflow.ID), fixedKeys())

	return m.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			return m.awaitingWriteCmd(
				"deleting "+workflow.Name,
				fmt.Sprintf("Could not delete workflow %q", workflow.Name),
				&awaits{gone: rowsIn(listArchivedWorkflows, workflow.ID)},
				func(a api) error { return a.deleteWorkflow(workflow.ID) },
			)
		}),
	})
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archivedAgents) stage(m *model) {
	m.stageTerminal()
}

func (a *archivedAgents) listParams(m *model) screenListParams {
	return screenListParams{
		help:     a.help,
		empty:    "nothing archived",
		rows:     len(a.listRows(m)),
		snapshot: func(pane listPane) view.Snapshot { return a.snapshot(m, pane) },
	}
}

func (a *archivedAgents) help(m *model) keyMap {
	return a.actions().help(m)
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
	_ listFilterer         = (*archivedAgents)(nil)
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

func (a *archivedAgents) hoveredKey(m *model) string {
	return a.list().hovered(m)
}

func (a *archivedAgents) hoveredName(m *model) string {
	return a.list().hoveredName(m)
}

func (a *archivedAgents) placeAt(m *model, key string) {
	a.list().place(m, key)
}

func (a *archivedAgents) filterRows(m *model) []filterRow {
	return a.list().filterRows(m)
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

// renameable is the row under the cursor when it can be renamed: an agent, a task, or a live workflow's folder.
func (a *archivedAgents) renameable(m *model) (agentRow, bool) {
	if a.selected.isActive() {
		return agentRow{}, false
	}

	row, ok := rowAt(a.listRows(m), a.cursor)
	if !ok || (row.header && !row.isTask() && row.workflow.ArchivedAt != nil) {
		return agentRow{}, false
	}

	return row, true
}

// openRename types over the name of the agent or live folder under the cursor.
func (a *archivedAgents) openRename(m *model) tea.Cmd {
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

func (a *archivedAgents) dispatches(*model) []dispatching {
	return []dispatching{a.actions()}
}
