package ui

import (
	"errors"
	"fmt"
	"strings"

	"weave/config"
	"weave/internal/tmux"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

func (m model) updateAgents(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if m.takeTopPress(k) {
		return m.jumpCursor(0), nil
	}
	if m.topHeld {
		return m, nil
	}
	if isBound(k, m.keys.Nav.Bottom) {
		return m.jumpCursor(len(m.agentRows()) - 1), nil
	}

	if sel, took := m.applySelectionKey(k, m.tree.selected, m.agentRows(), m.tree.cursor); took {
		m.tree.selected = sel

		return m, nil
	}

	if m.tree.selected.active() {
		return m.updateAgentsSelection(key)
	}

	if isBound(k, m.keys.Filter.Open) {
		return m.openFilter()
	}
	if m.filter.standing() && isBound(k, m.keys.Agents.Back) {
		return m.clearFilter()
	}
	if isBound(k, m.keys.Workflows.New) {
		return m.newWorkflowForm()
	}
	if isBound(k, m.keys.Agents.Search) {
		return m.enterSearch()
	}
	if isBound(k, m.keys.Schedules.Open) {
		return m.enterSchedules()
	}
	if isBound(k, m.keys.Archive.Open) {
		return m.enterArchive()
	}
	if isBound(k, m.keys.Agents.ShowHidden) {
		return m.toggleHiddenAgents()
	}
	if workflow, ok := m.hoveredWorkflow(); ok {
		return m.updateHoveredWorkflow(key, workflow)
	}

	switch {
	case isBound(k, m.keys.Agents.Quit), isBound(k, m.keys.Agents.Back):
		return m, m.hide()
	case isBound(k, m.keys.Agents.New):
		return m.openNewAgent(m.enclosingWorkflow())
	case isBound(k, m.keys.Nav.Up):
		return m.moveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveCursor(1), nil
	case isBound(k, m.keys.Agents.Info):
		return m.toggleRecord()
	case isBound(k, m.keys.Agents.Open):
		return m.openAgent()
	case isBound(k, m.keys.Agents.Editor):
		return m.runBelowAction(editorAction, "Could not open your editor")
	case isBound(k, m.keys.Agents.Terminal):
		return m.runBelowAction(terminalAction, "Could not open a shell")
	case isBound(k, m.keys.Actions.Open):
		return m.enterActions()
	}

	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	switch {
	case isBound(k, m.keys.Agents.Mark):
		return m.markAgent(item.agent)
	case isBound(k, m.keys.Agents.Hide):
		return m.hideAgent(item.agent)
	case isBound(k, m.keys.Agents.Rename):
		m.target.renameAgent = item.agent
		m.form, m.values.rename = renameForm(item.agent.Name, "", m.keys.Form)
		m.screen = screenRename

		return m, m.form.Init()
	case isBound(k, m.keys.Agents.Archive):
		return m.archiveAgents([]agentstore.Agent{item.agent})
	}

	return m, nil
}

func (m model) updateAgentsSelection(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Agents.Mark):
		return m.markSelection()
	case isBound(k, m.keys.Agents.Archive):
		return m.archiveAgents(chosenAgents(m.tree.selected, m.allAgentRows()))
	case isBound(k, m.keys.Agents.ShowHidden):
		return m.toggleHiddenAgents()
	case isBound(k, m.keys.Nav.Up):
		return m.moveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveCursor(1), nil
	}

	return m, nil
}

// A staged pane must be released before the service kills it.
func (m model) archiveAgents(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	for _, agent := range agents {
		m.stage.Release(agent.Pane)
	}

	return m, m.writeCmd(
		fmt.Sprintf("Could not archive %s", describeAgents(agents)),
		func(a api) error { return a.archiveAgents(agentIDs(agents)) },
	)
}

func (m model) moveCursor(delta int) model {
	next := m.tree.cursor + delta
	if next < 0 || next >= len(m.agentRows()) {
		return m
	}

	m.tree.cursor = next
	m.stageSelection()

	return m
}

func (m model) jumpCursor(idx int) model {
	rows := m.agentRows()
	if len(rows) == 0 {
		return m
	}

	m.tree.cursor = clampCursor(idx, len(rows))
	m.stageSelection()

	return m
}

func (m model) openAgent() (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}
	if item.alive {
		return m, nil
	}

	return m, m.resumeCmd(resumeParams{
		summary: fmt.Sprintf("Could not resume agent %q", item.agent.Name),
		id:      item.agent.ID,
	})
}

const (
	editorAction   = "_editor"
	terminalAction = "_terminal"
)

func (m model) toggleRecord() (tea.Model, tea.Cmd) {
	if m.tree.below == belowRecord {
		return m.closeBelow(), nil
	}
	if _, ok := m.selectedAgent(); !ok {
		return m, nil
	}

	m.tree.below = belowRecord
	m.tree.belowAction = ""
	m.stageSelection()

	return m, nil
}

func (m model) runBelowAction(name, summary string) (tea.Model, tea.Cmd) {
	if m.tree.below == belowAction && m.tree.belowAction == name {
		return m.closeBelow(), nil
	}

	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	if !m.claimBelow(belowAction, name) {
		return m, nil
	}

	agentID := item.agent.ID

	return m, m.writeCmd(summary, func(a api) error { return a.runAction(agentID, name) })
}

// Actions run out of process and read their target pane from tmux.
func (m *model) claimBelow(kind belowKind, name string) bool {
	pane := m.stage.OpenSecondary()
	if pane == "" {
		return false
	}

	m.tmux.SetPanelGlobal(tmux.SecondaryPaneOption, pane)
	m.stage.SecondaryClaimed()
	m.tree.below = kind
	m.tree.belowAction = name

	return true
}

// Clear the pane option because tmux can reuse pane IDs.
func (m model) closeBelow() model {
	m.stage.CloseSecondary()
	m.forgetBelow()

	return m
}

func (m *model) forgetBelow() {
	m.tmux.SetPanelGlobal(tmux.SecondaryPaneOption, "")
	m.tree.below = belowNothing
	m.tree.belowAction = ""
}

// Preserve the selected row when hidden rows change the list indexes.
func (m model) toggleHiddenAgents() (tea.Model, tea.Cmd) {
	selected := rowKeyAt(m.agentRows(), m.tree.cursor)

	m.tree.showHidden = !m.tree.showHidden

	rows := m.agentRows()
	if idx := rowIndexOfKey(rows, selected); idx >= 0 {
		m.tree.cursor = idx
	} else {
		m.tree.cursor = clampCursor(m.tree.cursor, len(rows))
	}
	m.tree.selected = m.tree.selected.pruned(m.allAgentRows())
	m.stageSelection()

	return m, nil
}

func (m model) markAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	next := nextMark(agent.Mark, m.marks)

	return m, m.writeCmd(
		fmt.Sprintf("Could not mark agent %q", agent.Name),
		func(a api) error { return a.setMarks([]string{agent.ID}, next) },
	)
}

func (m model) markSelection() (tea.Model, tea.Cmd) {
	ids := m.tree.selected.ids
	next := nextSelectionMark(selectionMarks(m.tree.selected, m.allAgentRows()), m.marks)

	return m, m.writeCmd(
		fmt.Sprintf("Could not mark %d agents", len(ids)),
		func(a api) error { return a.setMarks(ids, next) },
	)
}

func (m model) hideAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	failure := fmt.Sprintf("Could not hide agent %q", agent.Name)
	if agent.Hidden {
		failure = fmt.Sprintf("Could not unhide agent %q", agent.Name)
	}

	return m, m.writeCmd(failure, func(a api) error { return a.setHidden(agent, !agent.Hidden) })
}

func (m model) stageSelection() {
	rows := m.agentRows()
	if m.tree.cursor < 0 || m.tree.cursor >= len(rows) {
		m.stage.Blank()
		m.stageBelow()

		return
	}

	r := rows[m.tree.cursor]
	switch {
	case r.header:
		m.stage.Text(workflowMarkdown(r.workflow, m.agentCount(r.workflow.ID)))
	case r.item.alive:
		m.stage.Agent(r.item.agent.Pane)
	default:
		m.stage.Text(agentMarkdown(r.item.agent, false, m.scheduleName(r.item.agent)))
	}

	m.stageBelow()
}

func (m model) stageBelow() {
	switch m.tree.below {
	case belowRecord:
		rows := m.agentRows()
		if m.tree.cursor < 0 || m.tree.cursor >= len(rows) {
			return
		}

		if r := rows[m.tree.cursor]; !r.header {
			m.stage.SecondaryText(agentMarkdown(r.item.agent, r.item.alive, m.scheduleName(r.item.agent)))
		}
	case belowLogs:
		m.stage.SecondaryLog(m.logPath, logArgv(m.logViewer, m.logPath))
	case belowNothing, belowAction, belowEdit:
	}
}

func (m *model) reconcileBelow() {
	if m.tree.below == belowNothing || m.stage.IsSecondaryAlive() {
		return
	}

	m.forgetBelow()
}

func nextMark(current *string, marks []config.Mark) *string {
	if len(marks) == 0 {
		return nil
	}

	if current == nil {
		return &marks[0].Glyph
	}

	for i, mark := range marks {
		if mark.Glyph == *current && i+1 < len(marks) {
			return &marks[i+1].Glyph
		}
	}

	return nil
}

// Mixed selections collapse to the first mark so their next state is deterministic.
func nextSelectionMark(current []*string, marks []config.Mark) *string {
	if len(marks) == 0 || len(current) == 0 {
		return nil
	}

	for _, mark := range current[1:] {
		if !isSameMark(current[0], mark) {
			return &marks[0].Glyph
		}
	}

	return nextMark(current[0], marks)
}

func selectionMarks(sel selection, rows []agentRow) []*string {
	agents := chosenAgents(sel, rows)

	marks := make([]*string, 0, len(agents))
	for _, agent := range agents {
		marks = append(marks, agent.Mark)
	}

	return marks
}

func isSameMark(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
}

func (m model) scheduleName(agent agentstore.Agent) string {
	if agent.ScheduleID == nil {
		return ""
	}

	for _, item := range m.schedules.items {
		if item.schedule.ID == *agent.ScheduleID {
			return item.schedule.Name
		}
	}

	return ""
}

func (m model) agentsView() string {
	header := headerStyle.Render("Agents and Workflows") + "\n\n" + m.filterView()
	help := "\n" + m.renderHelp(m.agentHelp())

	all := m.agentRows()
	rows := make([]string, 0, len(all))
	for i, r := range all {
		hovered := i == m.tree.cursor
		prefix := "  "
		if hovered {
			prefix = cursorStyle.Render("› ")
		}
		prefix = selectionGutter(!r.header && m.tree.selected.has(r.item.agent.ID)) + prefix

		if r.header {
			label := m.filter.highlight(r.workflow.Name, nameStyle(hovered, false))
			rows = append(rows, prefix+dimStyle.Render(workflowIcon)+" "+label+hiddenTally(r.hiddenCount, m.tree.showHidden))
			continue
		}

		item := r.item
		state := item.agent.State
		if !item.alive {
			state = agentstore.StateIdle
		}

		name := m.filter.highlight(clipName(item.agent.Name), nameStyle(hovered, !item.alive))
		icon := lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
		rows = append(rows, prefix+"  "+icon+" "+name+scheduledMark(item.agent)+hiddenMark(item.agent.Hidden)+markGlyph(item.agent, m.marks))
	}

	return header + renderList(rows, m.tree.cursor, m.listLines(header, help)) + help
}

func (m model) openNewAgent(workflow *workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if workflow == nil {
		m.report("Could not start an agent", errNoWorkflow)

		return m, nil
	}

	m.target.newAgentWorkflow = workflow
	m.form, m.values.agent = newAgentForm(m.keys.Form, agentFormValues{Dir: workflow.Cwd, Flags: workflow.Flags})
	m.screen = screenForm

	return m, m.form.Init()
}

func (m model) updateRename(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		return m.submitRename()
	case huh.StateAborted:
		return m.closeForm()
	default:
		return m, cmd
	}
}

func (m model) submitRename() (tea.Model, tea.Cmd) {
	if m.values.rename == nil {
		return m.closeForm()
	}

	target := m.target.renameAgent
	name := strings.TrimSpace(*m.values.rename)
	cmd := m.writeCmd(
		fmt.Sprintf("Could not rename agent %q", target.Name),
		func(a api) error { return a.renameAgent(target, name) },
	)
	return m, tea.Batch(cmd, m.holdForm())
}

func (m model) submitAgent() (tea.Model, tea.Cmd) {
	if m.target.newAgentWorkflow == nil || m.values.agent == nil {
		m.report("Could not start the agent", errNoWorkflow)

		return m.closeForm()
	}

	params := createAgentParams{
		Prompt:     m.values.agent.Prompt,
		Title:      m.values.agent.Title,
		Dir:        m.currentDir(m.values.agent.Dir),
		Flags:      m.values.agent.Flags,
		WorkflowID: m.target.newAgentWorkflow.ID,
	}

	cmd := m.createCmd(params)

	return m, tea.Batch(cmd, m.holdForm())
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && isBound(key.String(), m.keys.Form.Submit) {
		return m.submitAgent()
	}

	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		return m.submitAgent()
	case huh.StateAborted:
		return m.closeForm()
	default:
		return m, cmd
	}
}
