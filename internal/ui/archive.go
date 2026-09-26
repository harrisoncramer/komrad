package ui

import (
	"fmt"
	"slices"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// archiveScreen is the archive screen: the cursor in the archived agents and how they are
// ordered.
type archiveScreen struct {
	rowCursor
	sort tabsState
	// rows is the rows built once for the frame being drawn, and nil between frames.
	rows []agentRow
}

// enter lands on the agent the target names.
func (a *archiveScreen) enter(h host, s panelShared, target screenTarget) tea.Cmd {
	if idx := rowIndexOfAgentID(a.listRows(s), target.agentID); target.agentID != "" && idx >= 0 {
		a.cursor = idx
	}

	a.stage(h, s)

	return nil
}

func (a *archiveScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return a.list().update(h, s, key, a.actions())
}

func (a *archiveScreen) list() screenList[agentRow] {
	return screenList[agentRow]{
		cursor: func() *rowCursor { return &a.rowCursor },
		rows:   a.listRows,
		name:   agentRowName,
		refs:   refsOf,
		header: "folder",
		placed: a.stage,
		moved: func(h host, s panelShared) tea.Cmd {
			a.stage(h, s)

			return h.waitToStage()
		},
		quit: func(k config.Keybindings) []string { return k.Archive.Quit },
		back: func(k config.Keybindings) []string { return k.Archive.Back },
	}
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
func (a *archiveScreen) listRows(s panelShared) []agentRow {
	if a.rows != nil {
		return a.rows
	}

	return a.buildRows(s)
}

func (a *archiveScreen) buildRows(s panelShared) []agentRow {
	// The archive draws an archived orchestrator as a row of its own.
	rows := treeRows(treeRowsParams{
		workflows:  archiveHeaders(s.listing),
		agents:     sortedAgents(s.listing.archivedAgents, archiveSorts, archiveTabs().order(a.sort)),
		showHidden: true,
		flat:       a.sort.flat,
	})

	return narrowedAgentRows(s.chrome.filter, rows)
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

func archivedByID(items []panel.AgentRow) map[string]panel.AgentRow {
	return xslice.KeyBy(items, func(item panel.AgentRow) string { return item.Agent.ID })
}

// archivedAgentsOf is the archive listing as bare rows.
func archivedAgentsOf(l listing) []panel.Agent {
	return xslice.Map(l.archivedAgents, func(item panel.AgentRow) panel.Agent { return item.Agent })
}

func (l listing) archivedCount(workflowID string) int {
	return xslice.Count(l.archivedAgents, func(item panel.AgentRow) bool { return item.Agent.WorkflowID == workflowID })
}

// place keeps the cursor on the row it was on as the rows under it change.
func (a *archiveScreen) place(s panelShared, hovered string) {
	rows := a.listRows(s)
	a.selected = a.selected.pruned(refsOf(rows))

	idx := rowIndexOfKey(rows, hovered)
	if idx < 0 {
		idx = clampCursor(a.cursor, len(rows))
	}

	a.cursor = idx
}

// hoveredAgent is the archived agent the cursor is on, and none on a folder.
func (a *archiveScreen) hoveredAgent(s panelShared) (panel.Agent, bool) {
	row, ok := rowAt(a.listRows(s), a.cursor)
	if !ok || row.header {
		return panel.Agent{}, false
	}

	return row.item.Agent, true
}

// hoveredWorkflow is the archived workflow the cursor is on, and none on an agent or on the
// folder of a live workflow.
func (a *archiveScreen) hoveredWorkflow(s panelShared) (panel.Workflow, bool) {
	workflow, ok := hoveredHeader(a.listRows(s), a.cursor)
	if !ok || workflow.ArchivedAt == nil {
		return panel.Workflow{}, false
	}

	return workflow, true
}

func (a *archiveScreen) restore(h host, s panelShared) tea.Cmd {
	if workflow, ok := a.hoveredWorkflow(s); ok {
		return h.writeCmd(
			fmt.Sprintf("Could not restore workflow %q", workflow.Name),
			func(c api) error { return c.restoreWorkflow(workflow.ID) },
		)
	}

	agent, ok := a.hoveredAgent(s)
	if !ok {
		return nil
	}

	return restoreAgentsCmd(h, []panel.Agent{agent})
}

// restoreAndOpen puts the hovered agent back and relaunches its conversation.
func (a *archiveScreen) restoreAndOpen(h host, s panelShared) tea.Cmd {
	agent, ok := a.hoveredAgent(s)
	if !ok {
		return nil
	}

	return restoreAndResumeCmd(h, agent)
}

// restoreAndResumeCmd takes one agent out of the archive and puts its conversation back in a
// pane.
func restoreAndResumeCmd(h host, agent panel.Agent) tea.Cmd {
	return h.openingWriteCmd(fmt.Sprintf("Could not restore and open agent %q", agent.Name), func(a api) (*agentv1.Agent, error) {
		if err := a.restoreAgents([]string{agent.ID}); err != nil {
			return nil, err
		}

		return a.resumeAgent(agent.ID)
	})
}

func restoreAgentsCmd(h host, agents []panel.Agent) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}

	return h.writeCmd(
		fmt.Sprintf("Could not restore %s", describeAgents(agents)),
		func(a api) error { return a.restoreAgents(panel.IDs(agents)) },
	)
}

// confirmDeleteArchived asks before the agents are deleted for good.
func confirmDeleteArchived(h host, s panelShared, agents []panel.Agent) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}

	form, confirmed := deleteAgentsForm(agents, unnamedChildren(archivedAgentsOf(s.listing), agents), formKeys())

	return h.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.writeCmd(
				fmt.Sprintf("Could not delete %s", describeAgents(agents)),
				func(a api) error { return a.deleteAgents(panel.IDs(agents)) },
			)
		}),
	})
}

// confirmDeleteWorkflow asks before an archived workflow is deleted for good.
func confirmDeleteWorkflow(h host, s panelShared, workflow panel.Workflow) tea.Cmd {
	form, confirmed := deleteWorkflowForm(workflow, s.listing.archivedCount(workflow.ID), formKeys())

	return h.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.writeCmd(
				fmt.Sprintf("Could not delete workflow %q", workflow.Name),
				func(a api) error { return a.deleteWorkflow(workflow.ID) },
			)
		}),
	})
}

// confirmEmptyArchive asks before every archived agent is deleted for good.
func confirmEmptyArchive(h host, s panelShared) tea.Cmd {
	count := len(s.listing.archivedAgents)
	if count == 0 {
		return nil
	}

	form, confirmed := emptyArchiveForm(count, formKeys())

	return h.openForm(formState{
		kind:   formArchiveEmpty,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.queuedCmd(
				"Could not empty the archive",
				message{
					kind:    messageInfo,
					summary: "Emptying the archive",
					detail:  fmt.Sprintf("A background job is deleting %s, each with its worktree. The rows are out of the archive now, and one whose delete fails comes back saying so.", label.Count(count, "archived agent", "archived agents")),
				},
				func(a api) error { return a.emptyArchive() },
			)
		}),
	})
}

func (a *archiveScreen) details(s panelShared) details {
	r, ok := rowAt(a.listRows(s), a.cursor)
	if !ok {
		return details{}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, s.listing.archivedCount(r.workflow.ID)),
		}
	}

	return details{
		id:      r.item.Agent.ID,
		notices: agentNotices(r),
		fields:  agentDetailFields(s, r.item),
		tag:     diffStatTag(agentWorktree(s.inspections, r.item.Agent.Cwd)),
	}
}

// stage keeps the info pane on the row the cursor is standing on.
func (a *archiveScreen) stage(h host, _ panelShared) {
	h.stageInfo()
}

// doc is the markdown drawn beside the list for the row the cursor is on.
func (a *archiveScreen) doc(s panelShared) string {
	r, ok := rowAt(a.listRows(s), a.cursor)
	switch {
	case !ok:
		return emptyArchiveMarkdown
	case r.header:
		return workflowContextMarkdown(r.workflow)
	default:
		return archiveDocument(s, r.item.Agent)
	}
}

// archiveDocument is what the panel draws beside an archived agent's row.
func archiveDocument(s panelShared, agent panel.Agent) string {
	if agent.State.IsFailed() {
		return failedPlaceholder(s, agent)
	}

	return archiveMarkdown(agent)
}

func (a *archiveScreen) view(s panelShared) string {
	a.rows = a.buildRows(s)
	defer func() { a.rows = nil }()

	return s.draw.columnListView(a.listParams(s))
}

func (a *archiveScreen) listParams(s panelShared) screenListParams {
	return screenListParams{
		title:    screenTitle(view.GlyphArchive, "Archive"),
		help:     a.help,
		empty:    "nothing archived",
		rows:     len(a.listRows(s)),
		snapshot: func(pane listPane) view.Snapshot { return a.snapshot(s, pane) },
	}
}

func (a *archiveScreen) help(s panelShared) keyMap {
	return a.actions().help(s)
}

// snapshot is the archive's rows as the plain data the shared drawing reads.
func (a *archiveScreen) snapshot(s panelShared, pane listPane) view.Snapshot {
	rows := a.listRows(s)

	return agentsSnapshot(snapshotParams{
		rows:          rows,
		marks:         s.listing.marks,
		selected:      a.selected,
		scheduled:     s.listing.scheduledWorkflows(),
		focused:       rowKeyAt(rows, a.cursor),
		caret:         pane.caret,
		barBlinkedOut: pane.blinkedOut,
		matcher:       s.chrome.filter.pattern,
		showHidden:    true,
		flat:          a.sort.flat,
		pane:          pane.width,
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
	_ detailed = (*archiveScreen)(nil)
	_ filtered = (*archiveScreen)(nil)
	_ tabbed   = (*archiveScreen)(nil)
	_ columned = (*archiveScreen)(nil)
	_ staged   = (*archiveScreen)(nil)
)

func (a *archiveScreen) doesDrawFromListing() bool {
	return true
}

func (a *archiveScreen) takesEveryKey() bool {
	return false
}

func (a *archiveScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Archive.Quit
}

func (a *archiveScreen) detailsFit(s panelShared) bool {
	return s.draw.listFitsDetails(a.listParams(s))
}

func (a *archiveScreen) detailsRoom(s panelShared) int {
	return s.draw.listRecordRoom(a.listParams(s))
}

func (a *archiveScreen) hoveredKey(s panelShared) string {
	return a.list().hovered(s)
}

func (a *archiveScreen) placeAt(h host, s panelShared, key string) {
	a.list().place(h, s, key)
}

func (a *archiveScreen) filterRows(s panelShared) []filterRow {
	return a.list().filterRows(s)
}

func (a *archiveScreen) tabs() tabStrip {
	return archiveTabs()
}

func (a *archiveScreen) tabsState() *tabsState {
	return &a.sort
}

func (a *archiveScreen) tabsFit(s panelShared) bool {
	return s.draw.listFitsTabs(a.listParams(s))
}

func (a *archiveScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   func(m model) string { return m.archive.doc(m.shared()) },
	}
}
