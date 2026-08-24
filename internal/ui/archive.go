package ui

import (
	"fmt"
	"slices"
	"strings"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
)

type archiveState struct {
	agents    []agentstore.Agent
	workflows []workflowstore.Workflow
	rowCursor
}

func (m model) enterArchive() (tea.Model, tea.Cmd) {
	m.enterScreen(screenArchive)
	m.stageArchive()

	return m, m.refreshCmd()
}

func (m model) archiveRows() []agentRow {
	return m.filter.filterAgentRows(m.allArchiveRows())
}

func (m model) allArchiveRows() []agentRow {
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

	slices.SortStableFunc(headers, func(a, b workflowstore.Workflow) int {
		return strings.Compare(a.ID, b.ID)
	})

	return headers
}

func (m *model) applyArchive(agents []agentstore.Agent, workflows []workflowstore.Workflow, selected string) {
	m.archive.agents, m.archive.workflows = agents, workflows

	m.archive.selected = m.archive.selected.pruned(m.allArchiveRows())

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

	if m.archive.selected.active() {
		return m.updateArchiveSelection(key)
	}

	switch {
	case isBound(k, m.keys.Filter.Open):
		return m.openFilter()
	case isBound(k, m.keys.Archive.Quit):
		return m, m.hide()
	case m.filter.standing() && isBound(k, m.keys.Archive.Back):
		return m.openFilter()
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

func (m model) moveArchiveCursor(delta int) model {
	next, ok := m.archive.move(delta, len(m.archiveRows()))
	if !ok {
		return m
	}

	m.archive.rowCursor = next
	m.stageArchive()

	return m
}

func (m model) jumpArchiveCursor(idx int) model {
	next, ok := m.archive.jump(idx, len(m.archiveRows()))
	if !ok {
		return m
	}

	m.archive.rowCursor = next
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

	m.target.deleteAgents = agents
	m.form, m.values.confirmed = deleteAgentsForm(agents, m.keys.Form)
	m.screen = screenArchiveDelete

	return m, m.form.Init()
}

func (m model) updateArchiveDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateConfirmForm(msg, confirmFormActions{
		write: func(m model) tea.Cmd {
			target := m.target.deleteAgents
			if len(target) == 0 || !m.wasConfirmed() {
				return nil
			}

			return m.writeCmd(
				fmt.Sprintf("Could not delete %s", describeAgents(target)),
				func(a api) error { return a.deleteAgents(agentIDs(target)) },
			)
		},
		clear: func(m model) model {
			m.target.deleteAgents = nil
			m.values.confirmed = nil

			return m
		},
		returnTo: func(m model) (tea.Model, tea.Cmd) {
			return m.enterArchive()
		},
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

func (m *model) stageArchive() {
	r, ok := rowAt(m.archiveRows(), m.archive.cursor)
	if !ok {
		m.stageDocument(emptyArchiveMarkdown)

		return
	}

	if r.header {
		m.stageDocument(workflowContextMarkdown(r.workflow))

		return
	}

	m.stageDocument(archiveMarkdown(r.item.agent))
}

func (m model) archivedCount(workflowID string) int {
	return xslice.Count(m.archive.agents, func(agent agentstore.Agent) bool { return agent.WorkflowID == workflowID })
}

func (m model) archiveView() string {
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
		prefix = selectionGutter(!r.header && m.archive.selected.has(r.item.agent.ID)) + prefix

		if r.header {
			label := m.filter.highlight(r.workflow.Name, m.nameStyle(hovered, false))
			rows = append(rows, prefix+dimStyle.Render(workflowIcon)+" "+label+archivedTag(r.workflow))

			continue
		}

		agent := r.item.agent
		name := m.filter.highlight(clipName(agent.Name), m.nameStyle(hovered, true))
		rows = append(rows, prefix+"  "+name+scheduledMark(agent)+tagGlyph(agent, m.tags))
	}

	return m.listView("Archive", rows, m.archive.cursor, below)
}
