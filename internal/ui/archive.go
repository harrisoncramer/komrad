package ui

import (
	"fmt"
	"slices"
	"strings"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// archiveState is the archive screen, the same tree of folders and agents the agents
// screen draws over what has been put away. Both listings are their own rather than a
// filter over the tree's, which holds the live rows only. Its focus is a raw agent id
// rather than a row key, since the archive draws no header worth landing a jump on.
type archiveState struct {
	agents    []agentstore.Agent
	workflows []workflowstore.Workflow
	rowCursor
}

// enterArchive switches to the archive, what has been put away. It is its own screen
// rather than a section of the agents tree because an archived agent is not
// something to act on alongside live work.
func (m model) enterArchive() (tea.Model, tea.Cmd) {
	m.enterScreen(screenArchive)
	m.stageArchive()

	return m, m.refreshCmd()
}

// archiveRows is the archive's tree as it is drawn, less whatever the filter holds back.
func (m model) archiveRows() []agentRow {
	return m.filter.filterAgentRows(m.allArchiveRows())
}

// allArchiveRows is the whole archive. Nothing in it is alive, since archiving kills the pane,
// and the hidden agents keep their rows, since the archive shows all it holds.
func (m model) allArchiveRows() []agentRow {
	items := xslice.Map(m.archive.agents, func(agent agentstore.Agent) agentItem { return agentItem{agent: agent} })

	return treeRows(m.archiveHeaders(), items, true)
}

// archiveHeaders is the folders the archive draws: every archived workflow, since an empty
// one still needs a row to be restored from, plus every live workflow holding an archived
// agent, as the folder those agents belong to. A live workflow is a header on both screens
// at once, carrying its live agents on one and its put-away ones here.
func (m model) archiveHeaders() []workflowstore.Workflow {
	holding := make(map[string]bool, len(m.archive.agents))
	for _, agent := range m.archive.agents {
		holding[agent.WorkflowID] = true
	}

	archived := make(map[string]bool, len(m.archive.workflows))
	for _, w := range m.archive.workflows {
		archived[w.ID] = true
	}

	headers := slices.Clone(m.archive.workflows)
	for _, w := range m.tree.workflows {
		// The two listings run at once, so a workflow restored between them can come back
		// in both, and a folder drawn twice would nest its agents twice.
		if holding[w.ID] && !archived[w.ID] {
			headers = append(headers, w)
		}
	}

	// Both screens order their folders by id, fixed at creation, so archiving one does not
	// move it out from under the cursor.
	slices.SortStableFunc(headers, func(a, b workflowstore.Workflow) int {
		return strings.Compare(a.ID, b.ID)
	})

	return headers
}

// applyArchive takes freshly-listed sets as the on-screen archive, keeping the cursor on
// the row selected names rather than on its index, so a row removed above it does not move
// the selection onto something else.
//
// The caller reads selected before the pass starts landing, since the archive draws headers
// for live folders too and those are replaced ahead of it.
func (m *model) applyArchive(agents []agentstore.Agent, workflows []workflowstore.Workflow, selected string) {
	m.archive.agents, m.archive.workflows = agents, workflows

	m.archive.selected = m.archive.selected.pruned(m.allArchiveRows())

	// A row a jump asked for takes the cursor for this listing only, and it is named by id
	// rather than by row key since the row carrying it only exists once this pass lands.
	focus := m.archive.focus
	m.archive.focus = ""

	rows := m.archiveRows()
	if idx := rowIndexOfAgentID(rows, focus); focus != "" && idx >= 0 {
		m.archive.cursor = idx

		return
	}

	if idx := rowIndexOfKey(rows, selected); idx >= 0 {
		m.archive.cursor = idx

		return
	}

	m.archive.cursor = clampCursor(m.archive.cursor, len(rows))
}

// updateArchive drives the archive. Nav moves the selection and restages, restore puts
// the hovered row back in the panel, and delete asks before destroying an agent.
func (m model) updateArchive(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// The ends of the list are answered ahead of everything else, since the top key holds a
	// press and every other key on the screen has to drop what it held.
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

	if m.archive.selected.active() {
		return m.updateArchiveSelection(key)
	}

	if cmd, ok := m.tryCopyRecordValue(k); ok {
		return m, cmd
	}

	switch {
	case isBound(k, m.keys.Filter.Open):
		return m.openFilter()
	case isBound(k, m.keys.Archive.Quit):
		return m, m.hide()
	case m.filter.standing() && isBound(k, m.keys.Archive.Back):
		return m.clearFilter()
	case isBound(k, m.keys.Archive.Back):
		return m.returnToAgents()
	case isBound(k, m.keys.Nav.Up):
		return m.moveArchiveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveArchiveCursor(1), nil
	case isBound(k, m.keys.Archive.Restore):
		return m.restoreFromArchive()
	case isBound(k, m.keys.Archive.Delete):
		agent, ok := m.selectedArchived()
		if !ok {
			return m, nil
		}

		return m.deleteFromArchiveConfirm([]agentstore.Agent{agent})
	}

	return m, nil
}

// updateArchiveSelection is what the archive still answers while a selection is up. Restore
// and delete act on the whole selection rather than on the row under the cursor, and the
// folder restore stands down, since a folder the cursor happens to rest on is not what the
// user chose.
func (m model) updateArchiveSelection(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Archive.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Archive.Restore):
		return m.restoreAgents(chosenAgents(m.archive.selected, m.allArchiveRows()))
	case isBound(k, m.keys.Archive.Delete):
		return m.deleteFromArchiveConfirm(chosenAgents(m.archive.selected, m.allArchiveRows()))
	case isBound(k, m.keys.Nav.Up):
		return m.moveArchiveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveArchiveCursor(1), nil
	}

	return m, nil
}

// moveArchiveCursor steps the archive cursor and restages what it lands on, doing nothing at
// either end of the list.
func (m model) moveArchiveCursor(delta int) model {
	next, ok := m.archive.move(delta, m.archiveRows())
	if !ok {
		return m
	}

	m.archive.rowCursor = next
	m.stageArchive()

	return m
}

// jumpArchiveCursor puts the archive cursor on a row by index, clamped to the list, which is
// how each end of it is reached.
func (m model) jumpArchiveCursor(idx int) model {
	next, ok := m.archive.jump(idx, m.archiveRows())
	if !ok {
		return m
	}

	m.archive.rowCursor = next
	m.stageArchive()

	return m
}

// selectedArchived returns the archived agent under the cursor, or false when the cursor
// is on a workflow header or the archive is empty.
func (m model) selectedArchived() (agentstore.Agent, bool) {
	row, ok := rowAt(m.archiveRows(), m.archive.cursor)
	if !ok || row.header {
		return agentstore.Agent{}, false
	}

	return row.item.agent, true
}

// hoveredArchivedWorkflow returns the archived workflow whose header the cursor is on. A
// live folder's header answers false, since it is drawn here to hold the agents put away
// out of it and there is nothing about the folder itself to bring back.
func (m model) hoveredArchivedWorkflow() (workflowstore.Workflow, bool) {
	workflow, ok := hoveredHeader(m.archiveRows(), m.archive.cursor)
	if !ok || workflow.ArchivedAt == nil {
		return workflowstore.Workflow{}, false
	}

	return workflow, true
}

// restoreFromArchive puts whatever the cursor is on back in the panel, dormant. A folder
// carries the agents archived inside it, the way one out of the trash carries its files,
// and an agent comes back on its own. Either way their conversations come back when the
// rows are opened from the tree.
//
// An agent may return renamed if a live one has taken its name, and it leaves the archive
// either way, so the list is re-read.
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

// restoreAgents puts agents back in the panel, dormant. It is all or nothing on the service
// side, so a refusal names what is in the way and the selection is still there once that is
// cleared.
func (m model) restoreAgents(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	return m, m.writeCmd(
		fmt.Sprintf("Could not restore %s", describeAgents(agents)),
		func(a api) error { return a.restoreAgents(agentIDs(agents)) },
	)
}

// deleteFromArchiveConfirm opens the prompt for destroying agents. This is the one thing in
// the panel nothing undoes, so it is the only key that asks first.
func (m model) deleteFromArchiveConfirm(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.target.deleteAgents = agents
	m.form, m.values.confirmed = deleteAgentsForm(agents, m.keys.Form)
	m.screen = screenArchiveDelete

	return m, m.form.Init()
}

// updateArchiveDelete drives the delete prompt. Confirming destroys the agent,
// declining and aborting leave it in the archive, and all three return to the
// archive rather than to the agents list, since that is where the key was pressed.
// Re-entering re-reads the list, so a destroyed agent is gone from it.
func (m model) updateArchiveDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		var write tea.Cmd
		if target := m.target.deleteAgents; len(target) > 0 && m.wasConfirmed() {
			write = m.writeCmd(
				fmt.Sprintf("Could not delete %s", describeAgents(target)),
				func(a api) error { return a.deleteAgents(agentIDs(target)) },
			)
		}
		m.target.deleteAgents = nil
		m.values.confirmed = nil
		entered, refresh := m.enterArchive()

		return entered, tea.Batch(write, refresh)
	case huh.StateAborted:
		m.target.deleteAgents = nil
		m.values.confirmed = nil
		return m.enterArchive()
	default:
		return m, cmd
	}
}

// stageArchive shows the hovered row's stored record in the stage. Nothing in the archive
// is running, so there is never a pane to swap in.
func (m *model) stageArchive() {
	r, ok := rowAt(m.archiveRows(), m.archive.cursor)
	if !ok {
		m.stagePlaceholder(emptyArchiveMarkdown)

		return
	}

	if r.header {
		m.stageRecord(workflowMarkdown(r.workflow, m.archivedCount(r.workflow.ID)), surfaceMain)

		return
	}

	m.stageRecord(archiveMarkdown(r.item.agent, m.scheduleName(r.item.agent)), surfaceMain)
}

// archivedCount is how many archived agents sit under a workflow, which is what a folder
// reports here rather than the live count the agents screen shows for the same folder.
func (m model) archivedCount(workflowID string) int {
	return xslice.Count(m.archive.agents, func(agent agentstore.Agent) bool { return agent.WorkflowID == workflowID })
}

// archiveView renders the archive's own pane, the tree of what has been put away.
func (m model) archiveView() string {
	help := "\n" + m.renderHelp(m.archiveHelp())

	all := m.archiveRows()
	if len(all) == 0 {
		return m.listView("Archive", []string{dimStyle.Render("nothing archived")}, 0, help)
	}

	rows := make([]string, 0, len(all))
	for i, r := range all {
		hovered := i == m.archive.cursor
		prefix := "  "
		if hovered {
			prefix = cursorStyle.Render("› ")
		}
		prefix = selectionGutter(!r.header && m.archive.selected.has(r.item.agent.ID)) + prefix

		if r.header {
			label := m.filter.highlight(r.workflow.Name, nameStyle(hovered, false))
			rows = append(rows, prefix+dimStyle.Render(workflowIcon)+" "+label+archivedTag(r.workflow))

			continue
		}

		agent := r.item.agent
		name := m.filter.highlight(clipName(agent.Name), nameStyle(hovered, true))
		rows = append(rows, prefix+"  "+name+scheduledMark(agent)+markGlyph(agent, m.marks))
	}

	return m.listView("Archive", rows, m.archive.cursor, help)
}
