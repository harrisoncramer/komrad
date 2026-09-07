package ui

import (
	"fmt"
	"slices"
	"strings"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type archiveState struct {
	agents    []agentstore.Agent
	workflows []workflowstore.Workflow
	// top is how far the document beside the list has been scrolled, held here rather than
	// worked out from the cursor because the reader moves it themselves.
	top int
	// topKey is the row that offset was scrolled on, kept because the cursor's own index slides
	// under a reader who has not moved whenever a row above them is archived or filtered away.
	topKey string
	rowCursor
}

// place moves the cursor and takes the document beside the list back to its top, since the offset
// was scrolled on the row the cursor is leaving.
func (a *archiveState) place(idx int, key string) {
	if key != a.topKey {
		a.top = 0
	}

	a.topKey = key
	a.cursor = idx
}

func (m model) enterArchive() (tea.Model, tea.Cmd) {
	m.enterScreen(screenArchive)
	m.stageArchive()

	return m, m.refreshCmd()
}

// soloArchive takes the window for the archive's own split, or hands it back while the info pane
// is up, since a sidebar holding the whole window covers the pane the logs are drawn in.
func (m *model) soloArchive() {
	// Every other screen answers for its own zoom, the review already and the search shortly, so
	// this says nothing at all about a window it is not the archive holding.
	if m.screen != screenArchive {
		return
	}

	m.frame.Solo(m.info.kind == infoNothing)
}

// archiveFilterRows is the archive's own rows with the name a search reads each by.
func (m model) archiveFilterRows() []filterRow {
	return filterRows(m.archiveRows())
}

func (m model) archiveRows() []agentRow {
	items := xslice.Map(m.archive.agents, func(agent agentstore.Agent) agentItem { return agentItem{agent: agent} })

	return treeRows(m.archiveHeaders(), items, true)
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
// list so the document's own offset is kept for as long as the reader is still on it.
func (m *model) landArchiveCursor(rows []agentRow, idx int) {
	m.archive.place(idx, rowKeyAt(rows, idx))
}

func (m *model) applyArchive(agents []agentstore.Agent, workflows []workflowstore.Workflow, selected string) {
	m.archive.agents, m.archive.workflows = agents, workflows

	m.archive.selected = m.archive.selected.pruned(m.archiveRows())

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

	if m.takeTopPress(k) {
		return m.jumpArchiveCursor(0), nil
	}
	if m.topHeld {
		return m, nil
	}
	if isBound(k, m.keys.Nav.Bottom) {
		return m.jumpArchiveCursor(len(m.archiveRows()) - 1), nil
	}

	if sel, took := m.applySelectionKey(k, m.archive.selected, m.archiveRows(), m.archive.cursor); took {
		m.archive.selected = sel

		return m, nil
	}

	if m.archive.selected.isActive() {
		return m.updateArchiveSelection(key)
	}

	switch {
	case isBound(k, m.keys.Filter.Open):
		return m.openFilter()
	case isBound(k, m.keys.Archive.Quit):
		return m, m.hide()
	case m.filter.isStanding() && isBound(k, m.keys.Filter.NextMatch):
		return m.jumpMatch(1)
	case m.filter.isStanding() && isBound(k, m.keys.Filter.PrevMatch):
		return m.jumpMatch(-1)
	case m.filter.isStanding() && isBound(k, m.keys.Archive.Back):
		return m.clearSearch()
	case isBound(k, m.keys.Archive.Back):
		return m.returnToAgents()
	case isBound(k, m.keys.Nav.Up):
		return m.moveArchiveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveArchiveCursor(1), nil
	case isBound(k, m.keys.Archive.ScrollDown):
		return m.scrollArchive(1)
	case isBound(k, m.keys.Archive.ScrollUp):
		return m.scrollArchive(-1)
	case isBound(k, m.keys.Archive.Restore):
		return m.restoreFromArchive()
	case isBound(k, m.keys.Archive.Delete):
		if workflow, ok := m.hoveredArchivedWorkflow(); ok {
			return m.deleteWorkflowConfirm(workflow)
		}

		agent, ok := m.selectedArchived()
		if !ok {
			return m, nil
		}

		return m.deleteFromArchiveConfirm([]agentstore.Agent{agent})
	case isBound(k, m.keys.Archive.Empty):
		return m.emptyArchiveConfirm()
	}

	return m, nil
}

func (m model) updateArchiveSelection(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Archive.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Archive.Restore):
		return m.restoreAgents(chosenAgents(m.archive.selected, m.archiveRows()))
	case isBound(k, m.keys.Archive.Delete):
		return m.deleteFromArchiveConfirm(chosenAgents(m.archive.selected, m.archiveRows()))
	case isBound(k, m.keys.Nav.Up):
		return m.moveArchiveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveArchiveCursor(1), nil
	}

	return m, nil
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

// scrollArchive moves the document beside the list, clamped to what is left of it so the reader
// cannot scroll a short document off the top of its own column.
func (m model) scrollArchive(direction int) (tea.Model, tea.Cmd) {
	split, ok := m.archiveSplit()
	if !ok {
		return m, nil
	}

	m.archive.top = clampDocTop(m.archive.top+direction*archiveScrollStep, len(m.archiveDocRows(split.diff)), split.rows)

	return m, nil
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

	form, confirmed := deleteAgentsForm(agents, unnamedChildren(m.archive.agents, agents), m.keys.Form)

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
	form, confirmed := deleteWorkflowForm(workflow, m.archivedCount(workflow.ID), m.keys.Form)

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

	form, confirmed := emptyArchiveForm(len(m.archive.agents), m.keys.Form)

	return m, m.openForm(formState{
		kind:   formArchiveEmpty,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.writeCmd(
				"Could not empty the archive",
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
			kind:   "Workflow",
			title:  r.workflow.Name,
			fields: workflowFields(r.workflow, m.archivedCount(r.workflow.ID)),
		}
	}

	return details{
		id:     r.item.agent.ID,
		kind:   "Agent",
		title:  r.item.agent.Name,
		fields: archiveFields(r.item.agent, m.scheduleName(r.item.agent)),
	}
}

// stageArchive keeps the info pane on the row the cursor is standing on, which is all there is
// left to stage now the document beside the list is drawn from that row rather than paged.
func (m *model) stageArchive() {
	m.stageInfo()
}

// archiveDoc is the markdown drawn beside the list: the notice for an empty archive, a workflow's
// shared context, or the archived agent the cursor is on.
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

// archiveDocument is what is drawn for an archived agent, which is why a delete failed when it
// failed, since the archive is the only place that can be read.
func (m model) archiveDocument(agent agentstore.Agent) string {
	if agent.State.IsFailed() {
		return m.failedPlaceholder(agent)
	}

	return archiveMarkdown(agent)
}

func (m model) archivedCount(workflowID string) int {
	return xslice.Count(m.archive.agents, func(agent agentstore.Agent) bool { return agent.WorkflowID == workflowID })
}

// archiveScrollStep is how far a scroll key moves the document, kept short of a screenful so the
// reader keeps a few lines of what they were already on.
const archiveScrollStep = 10

// archiveColumns is how the window would be divided while the archive holds it, which is the list
// on the panel's own side and the document beside it, and false while the screen is not up or the
// frame is sharing the window with a pane of somebody else's.
func (m model) archiveColumns() (reviewSplit, bool) {
	if m.screen != screenArchive || !m.frame.IsSolo() {
		return reviewSplit{}, false
	}

	return reviewSplitOf(m.width, m.height-m.bannerLines(), m.frame.SidebarWidth(m.width))
}

// archiveSplit is the window the archive is actually drawn across, which is archiveColumns
// refused again when the list would not fit the pane in the narrower column it leaves.
func (m model) archiveSplit() (reviewSplit, bool) {
	split, ok := m.archiveColumns()
	if !ok {
		return split, false
	}

	// The legend gives up its columns as the sidebar narrows and stands taller for it, so a window
	// the list fitted whole may not hold it once the document has taken half, and the list is
	// worth more whole at the full width than halved and running off the bottom.
	return split, countLines(m.at(split.sidebar).archiveListView()) <= split.rows
}

// archiveDocRows is the document rendered into its own column, a row at a time.
func (m model) archiveDocRows(width int) []string {
	return strings.Split(strings.TrimRight(render(m.archiveDoc(), width), "\n"), "\n")
}

// clampDocTop keeps an offset inside a document, since the cursor can land on a shorter one while
// the offset stands and a window past the end would draw nothing.
func clampDocTop(top, total, rows int) int {
	if total <= rows {
		return 0
	}

	// The marker saying what is above takes a row of its own once the reader has scrolled, so the
	// end of the document sits one row further down than the window is tall.
	return max(min(top, total-rows+1), 0)
}

// archiveDocView is the screenful of the document beside the list, from the row the reader has
// scrolled to, with a marker on each end counting what is past it, since a document cut to the
// window reads as a document that ended.
func (m model) archiveDocView(split reviewSplit) string {
	all := m.archiveDocRows(split.diff)
	top := clampDocTop(m.archive.top, len(all), split.rows)

	rows := make([]string, 0, split.rows)
	if top > 0 {
		rows = append(rows, dimStyle.Render(fmt.Sprintf("  ↑ %d more", top)))
	}

	// The marker under the text takes a row as well, so what is left over is one row shorter
	// again whenever the document runs past the window.
	end := min(top+max(split.rows-len(rows), 0), len(all))
	if end < len(all) {
		end = max(end-1, top)
	}

	rows = append(rows, all[top:end]...)
	if rest := len(all) - end; rest > 0 && len(rows) < split.rows {
		rows = append(rows, dimStyle.Render(fmt.Sprintf("  ↓ %d more", rest)))
	}
	// Padded out rather than left short, so a document smaller than the window keeps its half of
	// it instead of the list sliding under.
	for len(rows) < split.rows {
		rows = append(rows, "")
	}

	return strings.Join(rows, "\n")
}

func (m model) archiveView() string {
	split, ok := m.archiveSplit()
	if !ok {
		return m.archiveListView()
	}

	// Asked for before either half is drawn, so the halves are laid out already knowing their
	// column rather than sending every line of chrome back through paneWidth into the geometry.
	held := m.at(split.sidebar)

	// Trimmed so the two halves are the same height, since a trailing newline is a row the list
	// block would have and the document beside it would not.
	list := fitColumn(strings.TrimRight(held.archiveListView(), "\n"), split.sidebar)
	doc := fitColumn(held.archiveDocView(split), split.diff)

	if m.frame.IsSidebarOnRight() {
		return lipgloss.JoinHorizontal(lipgloss.Top, doc, list)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, list, doc)
}

func (m model) archiveListView() string {
	below := m.detailsView() + "\n" + m.renderHelp(m.archiveHelp())

	all := m.archiveRows()
	if len(all) == 0 {
		return m.listView("Archive", []string{dimStyle.Render("nothing archived")}, 0, below)
	}

	rows := make([]string, 0, len(all))
	for i, r := range all {
		hovered := i == m.archive.cursor
		prefix := "  "
		if hovered {
			prefix = m.rowCursorStyle().Render("› ")
		}

		if r.header {
			label := m.filter.highlight(r.workflow.Name, m.nameStyle(hovered, false))
			rows = append(rows, prefix+dimStyle.Render(workflowIcon)+" "+label+archivedTag(r.workflow))

			continue
		}

		agent := r.item.agent
		name := m.filter.highlight(clipName(agent.Name), m.nameStyle(hovered, true))
		row := prefix + "  " + childMark(r.child) + name + failedMark(agent) + scheduledMark(agent)
		if m.archive.selected.has(agent.ID) {
			row = paintRow(row, selectionBackground, m.paneWidth())
		}

		rows = append(rows, row)
	}

	return m.listView("Archive", rows, m.archive.cursor, below)
}
