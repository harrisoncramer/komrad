package ui

import (
	"fmt"
	"slices"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// archiveKind is which archived resources the archive is listing.
type archiveKind int

const (
	archiveAgents archiveKind = iota
	archiveSchedules
)

// archiveScreen is the archive screen: the cursor in the archived agents and how they are
// ordered, and the cursor in the archived schedules.
type archiveScreen struct {
	rowCursor
	sort      tabsState
	kind      archiveKind
	kindFocus bool
	schedules rowCursor
	// rows is the rows built once for the frame being drawn, and nil between frames.
	rows         []agentRow
	scheduleRows []scheduleItem
	renameSlot
}

// enter lands on the agent the target names.
func (a *archiveScreen) enter(m *model, target screenTarget) tea.Cmd {
	if idx := rowIndexOfAgentID(a.listRows(m), target.agentID); target.agentID != "" && idx >= 0 {
		a.kind = archiveAgents
		a.cursor = idx
	}

	a.stage(m)

	return m.waitToStage()
}

func (a *archiveScreen) update(m *model, msg tea.Msg) tea.Cmd {
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

	if a.kind == archiveSchedules {
		return a.scheduleList().update(m, key, a.actions())
	}

	return a.list().update(m, key, a.actions())
}

func (a *archiveScreen) onAgents(*model) bool {
	return a.kind == archiveAgents
}

func (a *archiveScreen) onSchedules(*model) bool {
	return a.kind == archiveSchedules
}

func (a *archiveScreen) kinds() stops {
	return stops{
		labels: []string{"agents", "schedules"},
		focus:  noStop,
		lit:    int(a.kind),
	}
}

func (a *archiveScreen) kindsFit(m *model) bool {
	return m.listFitsTabs(a.listParams(m))
}

func (a *archiveScreen) kindsFocused() *bool {
	return &a.kindFocus
}

// kindsSwitch holds the archive on its agents while any are selected.
func (a *archiveScreen) kindsSwitch(m *model) bool {
	return !a.onAgents(m) || !a.selected.isActive()
}

// stepKind moves the archive to another kind of archived resource.
func (a *archiveScreen) stepKind(m *model, delta int) tea.Cmd {
	a.kind = archiveKind(stepStop(int(a.kind), delta, len(a.kinds().labels)))

	a.stage(m)

	return m.waitToStage()
}

func (a *archiveScreen) list() screenList[agentRow] {
	return screenList[agentRow]{
		cursor: func() *rowCursor { return &a.rowCursor },
		rows:   a.listRows,
		name:   agentRowName,
		refs:   refsOf,
		header: "folder",
		placed: a.stage,
		moved: func(m *model) tea.Cmd {
			a.stage(m)

			return m.waitToStage()
		},
		quit: func(k config.Keybindings) []string { return k.Archive.Quit },
		back: func(k config.Keybindings) []string { return k.Archive.Back },
	}
}

func (a *archiveScreen) scheduleList() screenList[scheduleItem] {
	return screenList[scheduleItem]{
		cursor: func() *rowCursor { return &a.schedules },
		rows:   a.scheduleListRows,
		name:   func(item scheduleItem) string { return item.schedule.Name },
		placed: a.stage,
		moved: func(m *model) tea.Cmd {
			a.stage(m)

			return m.waitToStage()
		},
		quit: func(k config.Keybindings) []string { return k.Archive.Quit },
		back: func(k config.Keybindings) []string { return k.Archive.Back },
	}
}

// scheduleListRows is the archived schedules as drawn, or as they would be drawn when nothing has
// cached them.
func (a *archiveScreen) scheduleListRows(m *model) []scheduleItem {
	if a.scheduleRows != nil {
		return a.scheduleRows
	}

	return narrowed(m.filter, narrowedParams[scheduleItem]{
		rows:  m.listing.archivedSchedules,
		depth: func(scheduleItem) int { return 0 },
		name:  func(item scheduleItem) string { return item.schedule.Name },
	})
}

// hoveredSchedule is the archived schedule the cursor is on.
func (a *archiveScreen) hoveredSchedule(m *model) (panel.Schedule, bool) {
	item, ok := rowAt(a.scheduleListRows(m), a.schedules.cursor)

	return item.schedule, ok
}

// confirmDeleteSchedule asks before an archived schedule is deleted for good.
func confirmDeleteSchedule(m *model, schedule panel.Schedule) tea.Cmd {
	form, confirmed := deleteScheduleForm(schedule.Name, formKeys())

	return m.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			return m.writeCmd(
				"deleting "+schedule.Name,
				fmt.Sprintf("Could not delete schedule %q", schedule.Name),
				func(c api) error { return c.deleteSchedule(schedule.ID) },
			)
		}),
	})
}

// archiveSorts is every column the archive can be ordered by.
var archiveSorts = []sortColumn[panel.AgentRow]{
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

// listRows is the list as drawn, or as it would be drawn when nothing has cached it.
func (a *archiveScreen) listRows(m *model) []agentRow {
	if a.rows != nil {
		return a.rows
	}

	return a.buildRows(m)
}

func (a *archiveScreen) buildRows(m *model) []agentRow {
	// The archive draws an archived orchestrator as a row of its own.
	rows := treeRows(treeRowsParams{
		workflows:  archiveHeaders(m.listing),
		agents:     sortedAgents(m.listing.archivedAgents, archiveSorts, archiveTabs().order(a.sort)),
		showHidden: true,
		flat:       a.sort.flat,
	})

	return narrowedAgentRows(m.filter, rows)
}

// archiveHeaders is every workflow the archive draws a folder for: the archived ones, and the
// live ones still holding an archived agent.
func archiveHeaders(l listing) []panel.Workflow {
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

	// The merge draws from two listings, so the result is sorted again the way the query sorts.
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
func (a *archiveScreen) place(m *model, hovered string) {
	rows := a.listRows(m)
	a.selected = a.selected.pruned(refsOf(rows))

	a.cursor = cursor.Place(rowIndexOfKey(rows, hovered), a.cursor, len(rows))
	a.reconcileRename(m)
}

// placeSchedule keeps the cursor on the archived schedule it was on as the rows under it change.
func (a *archiveScreen) placeSchedule(m *model, hovered string) {
	rows := a.scheduleListRows(m)
	a.schedules.cursor = cursor.Place(rowIndexOfKey(rows, hovered), a.schedules.cursor, len(rows))
	a.reconcileRename(m)
}

// hoveredAgent is the archived agent the cursor is on, and none on a folder.
func (a *archiveScreen) hoveredAgent(m *model) (panel.Agent, bool) {
	row, ok := rowAt(a.listRows(m), a.cursor)
	if !ok || row.header {
		return panel.Agent{}, false
	}

	return row.item.Agent, true
}

// hoveredWorkflow is the archived workflow the cursor is on, and none on an agent or on the
// folder of a live workflow.
func (a *archiveScreen) hoveredWorkflow(m *model) (panel.Workflow, bool) {
	workflow, ok := hoveredHeader(a.listRows(m), a.cursor)
	if !ok || workflow.ArchivedAt == nil {
		return panel.Workflow{}, false
	}

	return workflow, true
}

// restoreAndResumeCmd takes one agent out of the archive and puts its conversation back in a
// pane.
func restoreAndResumeCmd(m *model, agent panel.Agent) tea.Cmd {
	return m.openingWriteCmd("restoring "+agent.Name, fmt.Sprintf("Could not restore and open agent %q", agent.Name), func(a api) (*agentv1.Agent, error) {
		if err := a.restoreAgents([]string{agent.ID}); err != nil {
			return nil, err
		}

		return a.resumeAgent(agent.ID)
	})
}

func restoreAgentsCmd(m *model, agents []panel.Agent) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}

	return m.writeCmd(
		"restoring "+describeAgents(agents),
		fmt.Sprintf("Could not restore %s", describeAgents(agents)),
		func(a api) error { return a.restoreAgents(panel.IDs(agents)) },
	)
}

// confirmDeleteArchived asks before the agents are deleted for good.
func confirmDeleteArchived(m *model, agents []panel.Agent) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}

	form, confirmed := deleteAgentsForm(agents, unnamedChildren(archivedAgentsOf(m.listing), agents), formKeys())

	return m.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			return m.writeCmd(
				"deleting "+describeAgents(agents),
				fmt.Sprintf("Could not delete %s", describeAgents(agents)),
				func(a api) error { return a.deleteAgents(panel.IDs(agents)) },
			)
		}),
	})
}

// confirmDeleteWorkflow asks before an archived workflow is deleted for good.
func confirmDeleteWorkflow(m *model, workflow panel.Workflow) tea.Cmd {
	form, confirmed := deleteWorkflowForm(workflow, m.listing.archivedCount(workflow.ID), formKeys())

	return m.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			return m.writeCmd(
				"deleting "+workflow.Name,
				fmt.Sprintf("Could not delete workflow %q", workflow.Name),
				func(a api) error { return a.deleteWorkflow(workflow.ID) },
			)
		}),
	})
}

func (a *archiveScreen) details(m *model) details {
	if a.kind == archiveSchedules {
		item, ok := rowAt(a.scheduleListRows(m), a.schedules.cursor)
		if !ok {
			return details{}
		}

		return scheduleDetails(m, item)
	}

	r, ok := rowAt(a.listRows(m), a.cursor)
	if !ok {
		return details{}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, m.listing.archivedCount(r.workflow.ID)),
		}
	}

	return details{
		id:      r.item.Agent.ID,
		notices: agentNotices(m.glyphs, m.loading.frame(), r),
		fields:  agentDetailFields(m, r.item),
	}
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archiveScreen) stage(m *model) {
	m.stageTerminal()
}

// doc is the markdown drawn beside the list for the row the cursor is on.
func (a *archiveScreen) doc(m *model) string {
	r, ok := rowAt(a.listRows(m), a.cursor)
	switch {
	case !ok:
		return emptyArchiveMarkdown
	case r.header:
		return workflowContextMarkdown(r.workflow)
	default:
		return archiveDocument(r.item.Agent)
	}
}

// docRows is the block drawn beside the list at a width for the row the cursor is on.
func (a *archiveScreen) docRows(m *model, width int) []string {
	if a.kind == archiveAgents {
		return paint.DocStyledRows(a.doc(m), width, columnStyle())
	}

	item, ok := rowAt(a.scheduleListRows(m), a.schedules.cursor)
	if !ok {
		return paint.DocStyledRows(emptyScheduleArchiveMarkdown, width, columnStyle())
	}

	return scheduleDocRows(item, width)
}

// archiveDocument is what the panel draws beside an archived agent's row.
func archiveDocument(agent panel.Agent) string {
	if agent.State.IsFailed() {
		return failedPlaceholder(agent)
	}

	return archiveMarkdown(agent)
}

func (a *archiveScreen) view(m *model) string {
	a.rows = a.buildRows(m)
	a.scheduleRows = a.scheduleListRows(m)
	defer func() { a.rows, a.scheduleRows = nil, nil }()

	return m.columnListView(a.listParams(m))
}

func (a *archiveScreen) listParams(m *model) screenListParams {
	if a.kind == archiveSchedules {
		return screenListParams{
			help: a.help,
			rows: len(a.scheduleListRows(m)),
			lines: func(pane listPane) ([]string, int) {
				return scheduleLines(m, pane, scheduleLinesParams{
					rows:   a.scheduleListRows(m),
					cursor: a.schedules.cursor,
					idle:   "no archived schedules",
				}), a.schedules.cursor
			},
		}
	}

	return screenListParams{
		help:     a.help,
		empty:    "nothing archived",
		rows:     len(a.listRows(m)),
		snapshot: func(pane listPane) view.Snapshot { return a.snapshot(m, pane) },
	}
}

func (a *archiveScreen) help(m *model) keyMap {
	return a.actions().help(m)
}

// snapshot is the archive's rows as the plain data the shared drawing reads.
func (a *archiveScreen) snapshot(m *model, pane listPane) view.Snapshot {
	rows := a.listRows(m)

	return agentsSnapshot(snapshotParams{
		rows:         rows,
		selected:     a.selected,
		scheduled:    m.listing.scheduledWorkflows(),
		workflowDirs: m.listing.workflowDirs(),
		focused:      rowKeyAt(rows, a.cursor),
		caret:        pane.caret,
		blinkedOut:   pane.blinkedOut,
		loaderFrame:  m.loading.frame(),
		matcher:      m.filter.pattern,
		renaming:     a.renamingField(),
		showHidden:   true,
		flat:         a.sort.flat,
		pane:         pane.width,
	})
}

func archiveTabs() tabStrip {
	return tabStrip{
		title:    "f",
		legend:   "sort",
		options:  tabOptionsOf(archiveSorts),
		fallback: "archived",
		grouped:  "workflow",
	}
}

var (
	_ recordDrawer         = (*archiveScreen)(nil)
	_ listFilterer         = (*archiveScreen)(nil)
	_ kindSwitcher         = (*archiveScreen)(nil)
	_ tabSwitcher          = (*archiveScreen)(nil)
	_ rowStager            = (*archiveScreen)(nil)
	_ hoveredNamer         = (*archiveScreen)(nil)
	_ itemMenuBuilder      = (*archiveScreen)(nil)
	_ loadingReporter      = (*archiveScreen)(nil)
	_ lineTyper            = (*archiveScreen)(nil)
	_ renamer              = (*archiveScreen)(nil)
	_ hoveredAgentReporter = (*archiveScreen)(nil)
	_ savedScreenDrawer    = (*archiveScreen)(nil)
)

func (a *archiveScreen) doesDrawFromListing() bool {
	return true
}

func (a *archiveScreen) takesEveryKey() bool {
	return a.isRenaming()
}

func (a *archiveScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Archive.Quit
}

func (a *archiveScreen) detailsFit(m *model) bool {
	return m.listFitsDetails(a.listParams(m))
}

func (a *archiveScreen) detailsRoom(m *model) int {
	return m.listRecordRoom(a.listParams(m))
}

func (a *archiveScreen) hoveredKey(m *model) string {
	if a.kind == archiveSchedules {
		return a.scheduleList().hovered(m)
	}

	return a.list().hovered(m)
}

func (a *archiveScreen) hoveredName(m *model) string {
	if a.kind == archiveSchedules {
		return a.scheduleList().hoveredName(m)
	}

	return a.list().hoveredName(m)
}

func (a *archiveScreen) placeAt(m *model, key string) {
	if a.kind == archiveSchedules {
		a.scheduleList().place(m, key)

		return
	}

	a.list().place(m, key)
}

func (a *archiveScreen) filterRows(m *model) []filterRow {
	if a.kind == archiveSchedules {
		return a.scheduleList().filterRows(m)
	}

	return a.list().filterRows(m)
}

func (a *archiveScreen) tabs() tabStrip {
	return archiveTabs()
}

func (a *archiveScreen) tabsState() *tabsState {
	return &a.sort
}

func (a *archiveScreen) tabsFit(m *model) bool {
	return a.kind == archiveAgents && m.listFitsTabs(a.listParams(m))
}

func (a *archiveScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		read:  isAlwaysRead,
		docRows: func(m *model, width int) []string {
			return m.withLastScreen(m.archive.docRows(m, width), width)
		},
	}
}

// renameable is the row under the cursor when it can be renamed: an agent, or a live workflow's folder.
func (a *archiveScreen) renameable(m *model) (agentRow, bool) {
	if a.onSchedules(m) || a.selected.isActive() {
		return agentRow{}, false
	}

	row, ok := rowAt(a.listRows(m), a.cursor)
	if !ok || (row.header && row.workflow.ArchivedAt != nil) {
		return agentRow{}, false
	}

	return row, true
}

// openRename types over the name of the agent or live folder under the cursor.
func (a *archiveScreen) openRename(m *model) tea.Cmd {
	if row, ok := a.renameable(m); ok {
		a.renaming = renameAgentRow(row)
	}

	return nil
}

func (a *archiveScreen) reconcileRename(m *model) {
	reconcileRename(&a.renameSlot, a.listRows(m), a.list().hovered(m), agentRowName)
}

func (a *archiveScreen) settleRename(m *model, row string, err error) {
	if a.settleField(row, err) {
		a.reconcileRename(m)
	}
}

func (a *archiveScreen) dispatches(*model) []dispatching {
	return []dispatching{a.actions()}
}
