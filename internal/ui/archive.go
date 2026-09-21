package ui

import (
	"fmt"
	"slices"

	"weave/config"
	"weave/internal/ui/paint/label"
	"weave/internal/ui/panel"
	"weave/internal/view"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// archiveScreen is the archive screen: the cursor in the archived agents, how they are ordered,
// and the rows built once for the frame being drawn.
type archiveScreen struct {
	rowCursor
	sort tabsState
	// rows is the rows built once for the frame being drawn, and nil between frames.
	rows []agentRow
}

// enterArchive is another screen's way onto the archive.
func (m model) enterArchive() (tea.Model, tea.Cmd) {
	cmd := m.enter(screenArchive, screenTarget{})

	return m, cmd
}

// enter lands on the agent the target names, remembering it as the focus for the listing the
// refresh brings back.
func (a *archiveScreen) enter(h host, s panelShared, target screenTarget) tea.Cmd {
	if target.agentID != "" {
		a.focus = target.agentID
		if idx := rowIndexOfAgentID(a.listRows(s), target.agentID); idx >= 0 {
			a.cursor = idx
		}
	}

	a.stage(h, s)

	return h.refreshCmd()
}

func (a *archiveScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
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
var archiveSorts = []sortColumn[agentItem]{
	{
		id:    "archived",
		label: "archived",
		has: func(item agentItem) bool {
			return item.agent.ArchivedAt != nil
		},
		compare: func(a, b agentItem) int {
			return a.agent.ArchivedAt.Compare(*b.agent.ArchivedAt)
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
	holding := xslice.KeyBy(l.archivedAgents, func(item agentItem) string { return item.agent.WorkflowID })
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

func archivedByID(items []agentItem) map[string]agentItem {
	return xslice.KeyBy(items, func(item agentItem) string { return item.agent.ID })
}

// archivedAgentsOf is the archive listing as bare rows.
func archivedAgentsOf(l listing) []panel.Agent {
	return xslice.Map(l.archivedAgents, func(item agentItem) panel.Agent { return item.agent })
}

func (l listing) archivedCount(workflowID string) int {
	return xslice.Count(l.archivedAgents, func(item agentItem) bool { return item.agent.WorkflowID == workflowID })
}

// applyArchive hands the screen a fresh listing and forgets what was answered for agents that
// have left it.
func (m *model) applyArchive(agents []agentItem, workflows []panel.Workflow, hovered string) {
	m.listing.archivedAgents, m.listing.archivedWorkflows = agents, workflows
	m.forgetUnlistedPullRequests()

	m.archive.apply(m, m.shared(), hovered)
}

// apply takes a fresh listing, keeping the cursor on the row it was on, or on the agent the
// screen was opened for.
func (a *archiveScreen) apply(h host, s panelShared, hovered string) {
	rows := a.listRows(s)
	a.selected = a.selected.pruned(refsOf(rows))

	focus := a.focus
	a.focus = ""

	idx := -1
	if focus != "" {
		idx = rowIndexOfAgentID(rows, focus)
	}
	if idx < 0 {
		idx = rowIndexOfKey(rows, hovered)
	}
	if idx < 0 {
		idx = clampCursor(a.cursor, len(rows))
	}

	a.cursor = idx
	h.placeColumn(screenArchive, rowKeyAt(rows, idx))
}

// hoveredAgent is the archived agent the cursor is on, and none on a folder.
func (a *archiveScreen) hoveredAgent(s panelShared) (panel.Agent, bool) {
	row, ok := rowAt(a.listRows(s), a.cursor)
	if !ok || row.header {
		return panel.Agent{}, false
	}

	return row.item.agent, true
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
		id:      r.item.agent.ID,
		notices: agentNotices(r),
		fields:  agentDetailFields(s, r.item),
	}
}

// stage keeps the notes and the info pane on the row the cursor is standing on.
func (a *archiveScreen) stage(h host, s panelShared) {
	h.placeColumn(screenArchive, a.list().hovered(s))
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
		return archiveDocument(s, r.item.agent)
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
		column:   "the notes",
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
		title:    "↑↓",
		legend:   "sort",
		options:  tabOptionsOf(archiveSorts),
		fallback: "archived",
		grouped:  "workflow",
	}
}

// archiveAdapter is the archive screen behind the panel's one door.
type archiveAdapter struct{}

var (
	_ detailed = archiveAdapter{}
	_ filtered = archiveAdapter{}
	_ tabbed   = archiveAdapter{}
	_ columned = archiveAdapter{}
	_ staged   = archiveAdapter{}
)

func (archiveAdapter) update(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.archive.update(&m, m.shared(), msg)

	return m, cmd
}

func (archiveAdapter) view(m model) string {
	return m.archive.view(m.shared())
}

func (archiveAdapter) needs() screenNeeds {
	return screenNeeds{fromListing: true, archive: true}
}

func (archiveAdapter) open(m model, target screenTarget) (tea.Model, tea.Cmd) {
	m.enterScreen(screenArchive)
	cmd := m.archive.enter(&m, m.shared(), target)

	return m, cmd
}

func (archiveAdapter) takesEveryKey(model) bool {
	return false
}

func (archiveAdapter) details(m model) details {
	return m.archive.details(m.shared())
}

func (archiveAdapter) detailsQuit(keys config.Keybindings) []string {
	return keys.Archive.Quit
}

func (archiveAdapter) detailsFit(m model) bool {
	s := m.shared()

	return s.draw.listFitsDetails(m.archive.listParams(s))
}

func (archiveAdapter) detailsRoom(m model) int {
	s := m.shared()

	return s.draw.listRecordRoom(m.archive.listParams(s))
}

func (archiveAdapter) hoveredKey(m model) string {
	return m.archive.list().hovered(m.shared())
}

func (archiveAdapter) placeAt(m *model, key string) {
	m.archive.list().place(m, m.shared(), key)
}

func (archiveAdapter) filterRows(m model) []filterRow {
	return m.archive.list().filterRows(m.shared())
}

func (archiveAdapter) tabs() tabStrip {
	return archiveTabs()
}

func (archiveAdapter) tabsState(m *model) *tabsState {
	return &m.archive.sort
}

func (archiveAdapter) tabsFit(m model) bool {
	s := m.shared()

	return s.draw.listFitsTabs(m.archive.listParams(s))
}

func (archiveAdapter) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   func(m model) string { return m.archive.doc(m.shared()) },
	}
}

func (archiveAdapter) stage(m *model) {
	m.archive.stage(m, m.shared())
}
