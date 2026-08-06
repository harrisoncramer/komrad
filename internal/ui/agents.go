package ui

import (
	"errors"
	"fmt"
	"strings"

	"weave/config"
	"weave/internal/tmux"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

// updateAgents handles the keys common to both agents-screen modes, then hands off to
// whichever mode the tree is currently in.
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

	return m.updateAgentsBrowsing(key)
}

// updateAgentsBrowsing handles the agents screen when no rows are selected: screen-level
// keys, then whatever the cursor is hovering over, then keys that act on the hovered agent.
func (m model) updateAgentsBrowsing(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if cmd, ok := m.tryCopyRecordValue(k); ok {
		return m, cmd
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
	case isBound(k, m.keys.Agents.Open):
		return m.openAgent()
	case isBound(k, m.keys.Actions.Open):
		return m.enterActions()
	}

	return m.updateHoveredAgent(key)
}

// updateHoveredAgent handles keys that act on the agent under the cursor. It is a no-op
// when the cursor is on a workflow header rather than an agent.
func (m model) updateHoveredAgent(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	return m.withSelectedAgent(func(item agentItem) (tea.Model, tea.Cmd) {
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
			return m.archiveAgentsConfirm([]agentstore.Agent{item.agent})
		}

		return m, nil
	})
}

// withSelectedAgent runs fn against the agent under the cursor, or is a no-op when the
// cursor is on a workflow header or the list is empty.
func (m model) withSelectedAgent(fn func(agentItem) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	return fn(item)
}

func (m model) updateAgentsSelection(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Agents.Mark):
		return m.markSelection()
	case isBound(k, m.keys.Agents.Archive):
		return m.archiveAgentsConfirm(chosenAgents(m.tree.selected, m.allAgentRows()))
	case isBound(k, m.keys.Agents.ShowHidden):
		return m.toggleHiddenAgents()
	case isBound(k, m.keys.Nav.Up):
		return m.moveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveCursor(1), nil
	}

	return m, nil
}

// archiveAgentsConfirm opens the archive prompt for one or more agents. Nothing is
// archived until the prompt is answered yes.
func (m model) archiveAgentsConfirm(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.target.archiveAgents = agents
	m.form, m.values.archive = archiveAgentsForm(agents, m.keys.Form)
	m.screen = screenAgentArchive

	return m, m.form.Init()
}

// updateAgentArchive drives the archive prompt. The answer decides whether the agents are put
// away, put away and then destroyed, or left alone, and every outcome returns to the list.
func (m model) updateAgentArchive(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		var write tea.Cmd
		if target := m.target.archiveAgents; len(target) > 0 {
			switch m.archiveAnswer() {
			case archiveChoiceArchive:
				_, write = m.archiveAgents(target)
			case archiveChoiceArchiveDelete:
				_, write = m.archiveAndDeleteAgents(target)
			case archiveChoiceCancel:
			}
		}
		m.target.archiveAgents = nil
		m.values.archive = nil
		returned, refresh := m.returnToAgents()

		return returned, tea.Batch(write, refresh)
	case huh.StateAborted:
		m.target.archiveAgents = nil
		m.values.archive = nil
		return m.returnToAgents()
	default:
		return m, cmd
	}
}

func (m model) archiveAgents(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.releasePanes(agents)

	return m, m.writeCmd(
		fmt.Sprintf("Could not archive %s", describeAgents(agents)),
		func(a api) error { return a.archiveAgents(agentIDs(agents)) },
	)
}

// archiveAndDeleteAgents puts agents away and then destroys them, the two calls in one write
// so a refused archive leaves the records alone rather than destroying what was never stopped.
func (m model) archiveAndDeleteAgents(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.releasePanes(agents)

	ids := agentIDs(agents)

	return m, m.writeCmd(
		fmt.Sprintf("Could not archive and delete %s", describeAgents(agents)),
		func(a api) error {
			if err := a.archiveAgents(ids); err != nil {
				return err
			}

			return a.deleteAgents(ids)
		},
	)
}

// A staged pane must be released before the service kills it.
func (m model) releasePanes(agents []agentstore.Agent) {
	for _, agent := range agents {
		m.stage.Release(agent.Pane)
	}
}

func (m model) moveCursor(delta int) model {
	next, ok := m.tree.move(delta, len(m.agentRows()))
	if !ok {
		return m
	}

	m.tree.rowCursor = next
	m.stageSelection()

	return m
}

func (m model) jumpCursor(idx int) model {
	next, ok := m.tree.jump(idx, len(m.agentRows()))
	if !ok {
		return m
	}

	m.tree.rowCursor = next
	m.stageSelection()

	return m
}

func (m model) openAgent() (tea.Model, tea.Cmd) {
	return m.withSelectedAgent(func(item agentItem) (tea.Model, tea.Cmd) {
		if item.alive {
			return m, nil
		}

		return m, m.resumeCmd(resumeParams{
			summary: fmt.Sprintf("Could not resume agent %q", item.agent.Name),
			id:      item.agent.ID,
		})
	})
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

// Clear the pane option because tmux can reuse pane IDs. Restaging afterward is what lets
// the agents list open its info pane back up when a log or an edit closes on an agent row,
// rather than leaving it closed until the next unrelated redraw finds it that way.
//
// The pane is left standing when restaging is about to fill it again. Killing it and
// splitting a new one in its place reflows the window twice, and a CLI in the stage answers
// each reflow with a full redraw, which is seen as the panes stuttering as the pane goes.
func (m model) closeBelow() model {
	if !m.reclaimsBelow() {
		m.stage.CloseSecondary()
	}

	m.forgetBelow()
	m.restageScreen()

	return m
}

// reclaimsBelow reports whether restaging would put something back in the pane under the
// stage, which is the agents list drawing a hovered agent's record there.
func (m model) reclaimsBelow() bool {
	if m.screen != screenAgents || m.tree.infoClosed {
		return false
	}

	r, ok := rowAt(m.agentRows(), m.tree.cursor)

	return ok && !r.header
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

// dormantPlaceholder is what the chat window shows in place of a pane for an agent with
// none running, the record's own notice and footer word for word, so the main pane and the
// record agree on what dormant means and how to leave it.
const dormantPlaceholder = "_dormant — not running_\n\nPress open to resume."

func (m *model) stageSelection() {
	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	m.syncInfoDefault(ok && !r.header)

	if !ok {
		m.stage.Blank()
		m.clearRecord()
		m.stageBelow()

		return
	}

	switch {
	case r.header:
		// A workflow has no chat of its own, so its record fills the main pane instead.
		m.stageRecord(workflowMarkdown(r.workflow, m.agentCount(r.workflow.ID)), surfaceMain)
	case r.item.alive:
		m.stage.Agent(r.item.agent.Pane)
		m.clearRecord()
	default:
		m.stagePlaceholder(dormantPlaceholder)
	}

	m.stageBelow()
}

// syncInfoDefault keeps the info pane's default open state in step with the row under the
// cursor. It opens on an agent row unless the user has dismissed it this session, and closes
// on a workflow header, which has no record of its own to hold there.
func (m *model) syncInfoDefault(onAgent bool) {
	switch {
	case onAgent && !m.tree.infoClosed && m.tree.below == belowNothing:
		m.tree.below = belowRecord
	case !onAgent:
		m.closeInfo()
	}
}

// closeInfo takes the info pane away if a record is what it is holding. A workflow header,
// and every screen but the agents list, has nothing of its own to put there, so this is what
// keeps the pane from holding onto whatever the agents list last opened in it.
func (m *model) closeInfo() {
	if m.tree.below != belowRecord {
		return
	}

	m.stage.CloseSecondary()
	m.forgetBelow()
}

func (m *model) stageBelow() {
	switch m.tree.below {
	case belowRecord:
		r, ok := rowAt(m.agentRows(), m.tree.cursor)
		if !ok || r.header {
			return
		}

		m.stageRecord(agentMarkdown(r.item.agent, r.item.alive, m.scheduleName(r.item.agent)), surfaceSecondary)
	case belowLogs:
		m.stage.SecondaryLog(m.logPath, logArgv(m.logViewer, m.logPath))
	case belowNothing, belowEdit:
	}
}

// reconcileBelow notices a pane under the stage that has exited on its own, tmux's own way
// of taking it away. A record closed this way is the user's way of dismissing it, so it is
// not put back the next time an agent is hovered.
func (m *model) reconcileBelow() {
	if m.tree.below == belowNothing || m.stage.IsSecondaryAlive() {
		return
	}

	if m.tree.below == belowRecord {
		m.tree.infoClosed = true
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

	return xslice.Map(agents, func(agent agentstore.Agent) *string { return agent.Mark })
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

	return m.listView("Agents and Workflows", rows, m.tree.cursor, help)
}

func (m model) openNewAgent(workflow *workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if workflow == nil {
		m.report("Could not start an agent", errNoWorkflow)

		return m, nil
	}

	m.target.newAgentWorkflow = workflow
	prefill := agentFormValues{
		Harness:       workflow.Harness,
		Dir:           workflow.Cwd,
		Flags:         workflow.Flags,
		SetupAction:   inheritAction,
		CleanupAction: inheritAction,
	}
	m.form, m.values.agent = newAgentForm(m.keys.Form, prefill, m.getAvailableActions())
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
		Prompt:        m.values.agent.Prompt,
		Title:         m.values.agent.Title,
		Harness:       m.values.agent.Harness,
		Dir:           m.currentDir(m.values.agent.Dir),
		Flags:         m.values.agent.Flags,
		SetupAction:   actionOverride(m.values.agent.SetupAction),
		CleanupAction: actionOverride(m.values.agent.CleanupAction),
		WorkflowID:    m.target.newAgentWorkflow.ID,
	}

	cmd := m.createCmd(params)

	return m, tea.Batch(cmd, m.holdForm())
}

func actionOverride(selected string) *string {
	if selected == inheritAction {
		return nil
	}

	return &selected
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
