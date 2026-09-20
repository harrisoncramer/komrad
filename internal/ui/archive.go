package ui

import (
	"fmt"
	"slices"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/paint"
	"weave/internal/ui/paint/label"
	"weave/internal/view"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// archiveDropped are the three columns the archive gives up: an archived agent's checkout, its
// review and its pull request are none of them refreshed once it is put away.
var archiveDropped = view.Columns{
	Repo:   true,
	Review: true,
	Forge:  true,
}

type archiveState struct {
	agents    []agentstore.Agent
	workflows []workflowstore.Workflow
	// top is how far the document beside the list has been scrolled, held here rather than
	rowCursor
}

func (m model) enterArchive() (tea.Model, tea.Cmd) {
	m.enterScreen(screenArchive)
	m.stageArchive()

	return m, m.refreshCmd()
}

// openArchive is the key's own way onto the screen, which lands with the search up.
func (m model) openArchive() (tea.Model, tea.Cmd) {
	m.enterScreen(screenArchive)
	m.beginFilter()
	m.stageArchive()

	return m, m.refreshCmd()
}

// archiveFilterRows is the archive's own rows with the name a search reads each by.
func (m model) archiveFilterRows() []filterRow {
	return filterRows(m.archiveRows())
}

func (m model) archiveRows() []agentRow {
	items := xslice.Map(m.archive.agents, func(agent agentstore.Agent) agentItem { return agentItem{agent: agent} })

	// The archive draws an archived orchestrator as a row of its own.
	rows := treeRows(treeRowsParams{
		workflows:  m.archiveHeaders(),
		agents:     items,
		showHidden: true,
	})

	return narrowedAgentRows(m.filter, rows)
}

func (m model) archiveHeaders() []workflowstore.Workflow {
	holding := xslice.KeyBy(m.archive.agents, func(agent agentstore.Agent) string { return agent.WorkflowID })
	archived := xslice.KeyBy(m.archive.workflows, func(w workflowstore.Workflow) string { return w.ID })

	headers := slices.Clone(m.archive.workflows)
	for _, w := range m.listing.workflows {
		_, isHolding := holding[w.ID]
		_, isArchived := archived[w.ID]
		if isHolding && !isArchived {
			headers = append(headers, w)
		}
	}

	// The merge draws from two listings, so the result has to be sorted again, and it is
	// sorted by the same comparator the query is written to reproduce.
	slices.SortStableFunc(headers, workflowstore.ByName)

	return headers
}

func (m model) archivedAgent(id string) (agentstore.Agent, bool) {
	for _, agent := range m.archive.agents {
		if agent.ID == id {
			return agent, true
		}
	}

	return agentstore.Agent{}, false
}

// hoveredArchiveKey is the row the cursor is on, read before a search moves it to a match.
func (m model) hoveredArchiveKey() string {
	return rowKeyAt(m.archiveRows(), m.archive.cursor)
}

func (m *model) placeArchiveCursor(hovered string) {
	rows := m.archiveRows()
	m.landArchiveCursor(rows, placeCursor(rowIndexOfKey(rows, hovered), m.archive.cursor, len(rows)))
	m.stageArchive()
}

// landArchiveCursor puts the cursor on a row, naming the row rather than only its place in the
// list so the document's own offset is kept.
func (m *model) landArchiveCursor(rows []agentRow, idx int) {
	m.archive.cursor = idx
	m.placeColumn(screenArchive, rowKeyAt(rows, idx))
}

func (m *model) applyArchive(agents []agentstore.Agent, workflows []workflowstore.Workflow, selected string) {
	m.archive.agents, m.archive.workflows = agents, workflows

	m.archive.selected = m.archive.selected.pruned(refsOf(m.archiveRows()))

	focus := m.archive.focus
	m.archive.focus = ""

	rows := m.archiveRows()
	if idx := rowIndexOfAgentID(rows, focus); focus != "" && idx >= 0 {
		m.landArchiveCursor(rows, idx)

		return
	}

	if idx := rowIndexOfKey(rows, selected); idx >= 0 {
		m.landArchiveCursor(rows, idx)

		return
	}

	m.landArchiveCursor(rows, clampCursor(m.archive.cursor, len(rows)))
}

func (m model) updateArchive(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// The doubled top key is a gesture rather than a binding, so it is settled before the table,
	// which would otherwise let a single press jump.
	if m.takeTopPress(k) {
		return m.jumpArchiveCursor(0), nil
	}
	if m.topHeld {
		return m, nil
	}

	return archiveActions().dispatch(m, k)
}

func (m model) moveArchiveCursor(delta int) model {
	rows := m.archiveRows()

	next, ok := m.archive.move(delta, len(rows))
	if !ok {
		return m
	}

	m.landArchiveCursor(rows, next.cursor)
	m.stageArchive()

	return m
}

func (m model) jumpArchiveCursor(idx int) model {
	rows := m.archiveRows()

	next, ok := m.archive.jump(idx, len(rows))
	if !ok {
		return m
	}

	m.landArchiveCursor(rows, next.cursor)
	m.stageArchive()

	return m
}

func (m model) selectedArchived() (agentstore.Agent, bool) {
	row, ok := rowAt(m.archiveRows(), m.archive.cursor)
	if !ok || row.header {
		return agentstore.Agent{}, false
	}

	return row.item.agent, true
}

func (m model) hoveredArchivedWorkflow() (workflowstore.Workflow, bool) {
	workflow, ok := hoveredHeader(m.archiveRows(), m.archive.cursor)
	if !ok || workflow.ArchivedAt == nil {
		return workflowstore.Workflow{}, false
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

	return m.restoreAgents([]agentstore.Agent{agent})
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
// pane, landing the panel on the row it made.
func (m *model) restoreAndResumeCmd(agent agentstore.Agent) tea.Cmd {
	return m.openingWriteCmd(fmt.Sprintf("Could not restore and open agent %q", agent.Name), func(a api) (*agentv1.Agent, error) {
		if err := a.restoreAgents([]string{agent.ID}); err != nil {
			return nil, err
		}

		return a.resumeAgent(agent.ID)
	})
}

func (m model) restoreAgents(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	return m, m.writeCmd(
		fmt.Sprintf("Could not restore %s", describeAgents(agents)),
		func(a api) error { return a.restoreAgents(agentIDs(agents)) },
	)
}

func (m model) deleteFromArchiveConfirm(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	form, confirmed := deleteAgentsForm(agents, unnamedChildren(m.archive.agents, agents), formKeys())

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
				func(a api) error { return a.deleteAgents(agentIDs(agents)) },
			)
		}),
	})
}

func (m model) deleteWorkflowConfirm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
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
	if len(m.archive.agents) == 0 {
		return m, nil
	}

	form, confirmed := emptyArchiveForm(len(m.archive.agents), formKeys())

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
					detail:  fmt.Sprintf("A background job is deleting %s, each with its worktree. The rows leave the archive as it goes.", label.Count(len(m.archive.agents), "archived agent", "archived agents")),
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
		fields:  archiveFields(r.item.agent, m.scheduleName(r.item.agent)),
	}
}

// stageArchive keeps the info pane on the row the cursor is standing on.
func (m *model) stageArchive() {
	m.stageInfo()
}

// archiveDoc is the markdown drawn beside the list: the notice for an empty archive, a
// workflow's shared context, or the archived agent the cursor is on.
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
func (m model) archiveDocument(agent agentstore.Agent) string {
	if agent.State.IsFailed() {
		return m.failedPlaceholder(agent)
	}

	return archiveMarkdown(agent)
}

func (m model) archivedCount(workflowID string) int {
	return xslice.Count(m.archive.agents, func(agent agentstore.Agent) bool { return agent.WorkflowID == workflowID })
}

// archiveColumn is the archived agent's own notes drawn beside the list, refused when the list
// would not fit the narrower column the split leaves it.
func archiveColumn() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   model.archiveDoc,
	}
}

func (m model) archiveView() string {
	return m.columnView(model.archiveListView)
}

// archiveSnapshot is the archive's rows as the plain data the shared drawing reads.
func (m model) archiveSnapshot(rows []agentRow, pane int) view.Snapshot {
	return agentsSnapshot(snapshotParams{
		rows:          rows,
		marks:         m.listing.marks,
		selected:      m.archive.selected,
		scheduled:     m.scheduledWorkflows(),
		focused:       rowKeyAt(rows, m.archive.cursor),
		caretHidden:   m.cursorBlinkedOut(m.isListFocused()),
		barBlinkedOut: m.rowCursor.Blink,
		matcher:       m.filter.pattern,
		showHidden:    true,
		pane:          pane,
		dropped:       archiveDropped,
	})
}

func (m model) archiveHeader() string {
	return m.listHeader("Archive")
}

func (m model) doesArchiveFitDetails() bool {
	return m.doesListFitDetails(m.archiveHeader(), model.archiveHelp)
}

func (m model) archiveListView() string {
	header := m.archiveHeader()
	record, help := m.detailsView(), m.renderHelp(m.archiveHelp(), header)
	below := record + m.messagesRegion(record, help, header) + help

	rows := m.archiveRows()
	if len(rows) == 0 {
		return m.headedListView(header, []string{dimStyle.Render(m.emptyListLine("nothing archived"))}, 0, below)
	}

	palette := paint.Panel
	pane := m.paneWidth()

	list := view.Prepare(m.archiveSnapshot(rows, pane))

	lines := m.listLines(header, below)
	window := view.Window(view.WindowParams{
		List:    list,
		Palette: palette,
		Width:   pane,
		Lines:   lines,
	})

	return m.headedBlockView(header, window, lines, below)
}
