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

// archiveDropped are the three columns the archive gives up.
var archiveDropped = view.Columns{
	Repo:   true,
	Review: true,
	Forge:  true,
}

type archiveState struct {
	sort tabsState
	rows []agentRow
	rowCursor
}

func archiveList() listSpec[agentRow] {
	return listSpec[agentRow]{
		cursor: func(m *model) *rowCursor { return &m.archive.rowCursor },
		rows:   model.archiveRows,
		name:   agentRowName,
		refs:   refsOf,
		header: "folder",
		placed: (*model).stageArchive,
		quit:   func(k config.Keybindings) []string { return k.Archive.Quit },
		back:   func(k config.Keybindings) []string { return k.Archive.Back },
	}
}

func archiveChrome() listChrome {
	return listChrome{
		title: screenTitle(view.GlyphArchive, "Archive"),
		help:  model.archiveHelp,
	}
}

func (m model) enterArchive() (tea.Model, tea.Cmd) {
	m.enterScreen(screenArchive)
	m.stageArchive()

	return m, m.refreshCmd()
}

// openArchive is the key's own way onto the screen, landing with the search up.
func (m model) openArchive() (tea.Model, tea.Cmd) {
	m.enterScreen(screenArchive)
	m.beginFilter()
	m.stageArchive()

	return m, m.refreshCmd()
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
		id:        "name",
		label:     "name",
		direction: tabUp,
		compare:   byAgentName,
	},
}

func (m model) archiveRows() []agentRow {
	if m.archive.rows != nil {
		return m.archive.rows
	}

	return m.buildArchiveRows()
}

func (m model) buildArchiveRows() []agentRow {
	items := xslice.Map(m.listing.archivedAgents, func(agent panel.Agent) agentItem { return agentItem{agent: agent} })

	// The archive draws an archived orchestrator as a row of its own.
	rows := treeRows(treeRowsParams{
		workflows:  m.archiveHeaders(),
		agents:     sortedAgents(items, archiveSorts, archiveTabs().order(m.archive.sort)),
		showHidden: true,
		flat:       m.archive.sort.flat,
	})

	return narrowedAgentRows(m.filter, rows)
}

func (m model) archiveHeaders() []panel.Workflow {
	holding := xslice.KeyBy(m.listing.archivedAgents, func(agent panel.Agent) string { return agent.WorkflowID })
	archived := xslice.KeyBy(m.listing.archivedWorkflows, func(w panel.Workflow) string { return w.ID })

	headers := slices.Clone(m.listing.archivedWorkflows)
	for _, w := range m.listing.workflows {
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

func archivedByID(agents []panel.Agent) map[string]panel.Agent {
	return xslice.KeyBy(agents, func(agent panel.Agent) string { return agent.ID })
}

func (m model) archivedAgent(id string) (panel.Agent, bool) {
	for _, agent := range m.listing.archivedAgents {
		if agent.ID == id {
			return agent, true
		}
	}

	return panel.Agent{}, false
}

func (m *model) applyArchive(agents []panel.Agent, workflows []panel.Workflow, selected string) {
	m.listing.archivedAgents, m.listing.archivedWorkflows = agents, workflows

	rows := m.archiveRows()
	m.archive.selected = m.archive.selected.pruned(refsOf(rows))

	focus := m.archive.focus
	m.archive.focus = ""

	idx := -1
	if focus != "" {
		idx = rowIndexOfAgentID(rows, focus)
	}
	if idx < 0 {
		idx = rowIndexOfKey(rows, selected)
	}
	if idx < 0 {
		idx = clampCursor(m.archive.cursor, len(rows))
	}

	m.archive.cursor = idx
	m.placeColumn(screenArchive, rowKeyAt(rows, idx))
}

func (m model) updateArchive(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return archiveList().update(m, key, archiveActions())
}

func (m model) selectedArchived() (panel.Agent, bool) {
	row, ok := rowAt(m.archiveRows(), m.archive.cursor)
	if !ok || row.header {
		return panel.Agent{}, false
	}

	return row.item.agent, true
}

func (m model) hoveredArchivedWorkflow() (panel.Workflow, bool) {
	workflow, ok := hoveredHeader(m.archiveRows(), m.archive.cursor)
	if !ok || workflow.ArchivedAt == nil {
		return panel.Workflow{}, false
	}

	return workflow, true
}

func (m model) restoreFromArchive() (tea.Model, tea.Cmd) {
	if workflow, ok := m.hoveredArchivedWorkflow(); ok {
		return m, m.writeCmd(
			fmt.Sprintf("Could not restore workflow %q", workflow.Name),
			func(a api) error { return a.restoreWorkflow(workflow.ID) },
		)
	}

	agent, ok := m.selectedArchived()
	if !ok {
		return m, nil
	}

	return m.restoreAgents([]panel.Agent{agent})
}

// restoreAndOpen puts the hovered agent back and relaunches its conversation.
func (m model) restoreAndOpen() (tea.Model, tea.Cmd) {
	agent, ok := m.selectedArchived()
	if !ok {
		return m, nil
	}

	return m, m.restoreAndResumeCmd(agent)
}

// restoreAndResumeCmd takes one agent out of the archive and puts its conversation back in a
// pane.
func (m *model) restoreAndResumeCmd(agent panel.Agent) tea.Cmd {
	return m.openingWriteCmd(fmt.Sprintf("Could not restore and open agent %q", agent.Name), func(a api) (*agentv1.Agent, error) {
		if err := a.restoreAgents([]string{agent.ID}); err != nil {
			return nil, err
		}

		return a.resumeAgent(agent.ID)
	})
}

func (m model) restoreAgents(agents []panel.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	return m, m.writeCmd(
		fmt.Sprintf("Could not restore %s", describeAgents(agents)),
		func(a api) error { return a.restoreAgents(panel.IDs(agents)) },
	)
}

func (m model) deleteFromArchiveConfirm(agents []panel.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	form, confirmed := deleteAgentsForm(agents, unnamedChildren(m.listing.archivedAgents, agents), formKeys())

	return m, m.openForm(formState{
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

func (m model) deleteWorkflowConfirm(workflow panel.Workflow) (tea.Model, tea.Cmd) {
	form, confirmed := deleteWorkflowForm(workflow, m.archivedCount(workflow.ID), formKeys())

	return m, m.openForm(formState{
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

func (m model) emptyArchiveConfirm() (tea.Model, tea.Cmd) {
	if len(m.listing.archivedAgents) == 0 {
		return m, nil
	}

	form, confirmed := emptyArchiveForm(len(m.listing.archivedAgents), formKeys())

	return m, m.openForm(formState{
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
					detail:  fmt.Sprintf("A background job is deleting %s, each with its worktree. The rows are out of the archive now, and one whose delete fails comes back saying so.", label.Count(len(m.listing.archivedAgents), "archived agent", "archived agents")),
				},
				func(a api) error { return a.emptyArchive() },
			)
		}),
	})
}

func (m model) archiveDetails() details {
	r, ok := rowAt(m.archiveRows(), m.archive.cursor)
	if !ok {
		return details{}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, m.archivedCount(r.workflow.ID)),
		}
	}

	return details{
		id:      r.item.agent.ID,
		notices: failureNotices(r),
		fields:  archiveFields(r.item.agent, m.workflowName(r.item.agent.WorkflowID), m.scheduleName(r.item.agent)),
	}
}

// stageArchive keeps the notes and the info pane on the row the cursor is standing on.
func (m *model) stageArchive() {
	m.placeColumn(screenArchive, archiveList().hovered(*m))
	m.stageInfo()
}

// archiveDoc is the markdown drawn beside the list for the row the cursor is on.
func (m model) archiveDoc() string {
	r, ok := rowAt(m.archiveRows(), m.archive.cursor)
	switch {
	case !ok:
		return emptyArchiveMarkdown
	case r.header:
		return workflowContextMarkdown(r.workflow)
	default:
		return m.archiveDocument(r.item.agent)
	}
}

// archiveDocument is what the panel draws beside an archived agent's row.
func (m model) archiveDocument(agent panel.Agent) string {
	if agent.State.IsFailed() {
		return m.failedPlaceholder(agent)
	}

	return archiveMarkdown(agent)
}

func (m model) archivedCount(workflowID string) int {
	return xslice.Count(m.listing.archivedAgents, func(agent panel.Agent) bool { return agent.WorkflowID == workflowID })
}

// archiveColumn is the archived agent's own notes drawn beside the list.
func archiveColumn() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   model.archiveDoc,
	}
}

func (m model) archiveView() string {
	m.archive.rows = m.buildArchiveRows()

	return m.columnView(model.archiveListView)
}

func (m model) archiveListView() string {
	return m.listView(listViewParams{
		chrome:   archiveChrome(),
		empty:    "nothing archived",
		rows:     len(m.archiveRows()),
		snapshot: model.archiveSnapshot,
	})
}

// archiveSnapshot is the archive's rows as the plain data the shared drawing reads.
func (m model) archiveSnapshot(pane int) view.Snapshot {
	rows := m.archiveRows()

	return agentsSnapshot(snapshotParams{
		rows:          rows,
		marks:         m.listing.marks,
		selected:      m.archive.selected,
		scheduled:     m.scheduledWorkflows(),
		focused:       rowKeyAt(rows, m.archive.cursor),
		caretHidden:   m.isCursorBlinkedOut(m.isListFocused()),
		barBlinkedOut: m.rowCursor.Blink,
		matcher:       m.filter.pattern,
		showHidden:    true,
		flat:          m.archive.sort.flat,
		pane:          pane,
		dropped:       archiveDropped,
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
