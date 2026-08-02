package ui

import (
	"errors"
	"fmt"
	"strings"

	"weave/internal/appconfig"
	"weave/internal/tmux"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// errNoWorkflow is what the panel reports when a create is attempted with no
// workflow to put the agent in, which only happens when none exist at all.
var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

// updateAgents drives the nav list. The nav keys move the selection and restage,
// and letter keys act on the hovered agent or workflow.
func (m model) updateAgents(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// The ends of the list are answered ahead of everything else, since the top key holds a
	// press and every other key on the screen has to drop what it held.
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

	// A selection takes the list over. While one is up the keys that only make sense on a
	// single row stand down, since acting on the hovered one alone would be the wrong thing
	// done quietly.
	if m.tree.selected.active() {
		return m.updateAgentsSelection(key)
	}

	// These act on the whole view rather than the row under the cursor, so they are
	// checked before anything that reads the cursor.
	if isBound(k, m.keys.Filter.Open) {
		return m.openFilter()
	}
	// A narrowed list is a mode to leave before the screen is, so the back key clears the
	// filter first rather than taking the panel down over it.
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
	if isBound(k, m.keys.Agents.Logs) {
		return m.openLogs()
	}
	// On a workflow header the workflow bindings act on that folder's workflow.
	if workflow, ok := m.hoveredWorkflow(); ok {
		return m.updateHoveredWorkflow(key, workflow)
	}

	switch {
	case isBound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Agents.Back):
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
	case isBound(k, m.keys.Agents.Mark):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		return m.markAgent(item.agent)
	case isBound(k, m.keys.Agents.Hide):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		return m.hideAgent(item.agent)
	case isBound(k, m.keys.Agents.Rename):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		m.target.renameAgent = item.agent
		m.form, m.values.rename = renameForm(item.agent.Name, "", m.keys.Form)
		m.screen = screenRename
		return m, m.form.Init()
	case isBound(k, m.keys.Agents.Archive):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}

		return m.archiveAgents([]agentstore.Agent{item.agent})
	}

	return m, nil
}

// updateAgentsSelection is what the agents list still answers while a selection is up, past
// the selection keys its caller has already taken. Movement and the way out stay live,
// along with the mark and archive keys, which act on the whole selection. So does the hidden
// toggle, which changes what is drawn rather than acting on a row and is the one key that
// can put a chosen agent off screen.
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

// archiveAgents stops agents and moves them to the archive. The panes are taken out of the
// stage first, since killing one while it sits in the stage slot would take the stage down
// with it.
//
// The selection is left as it is. An agent that went loses its row and comes off the
// selection on the next listing, and one the service refused is still chosen to try again.
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

// moveCursor steps the tree cursor and restages what it lands on, doing nothing at either
// end of the list.
func (m model) moveCursor(delta int) model {
	next := m.tree.cursor + delta
	if next < 0 || next >= len(m.agentRows()) {
		return m
	}

	m.tree.cursor = next
	m.stageSelection()

	return m
}

// jumpCursor puts the tree cursor on a row by index, clamped to the list, which is how each
// end of it is reached.
func (m model) jumpCursor(idx int) model {
	rows := m.agentRows()
	if len(rows) == 0 {
		return m
	}

	m.tree.cursor = clampCursor(idx, len(rows))
	m.stageSelection()

	return m
}

// openAgent acts on the hovered agent. A dormant one is restored and staged live, and a
// live one is already staged from hovering, so opening it has nothing left to do.
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

// The actions the panel's own keys run. Each is shipped with weave and replaced by a file
// of the same name in the user's actions directory, which is how a key is given different
// behaviour without a setting for it.
const (
	editorAction   = "_editor"
	terminalAction = "_terminal"
)

// toggleRecord shows the hovered agent's stored record under the stage, and takes it away
// again on a second press. It goes below rather than into the stage so the agent it is
// about stays on screen beside it.
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

// runBelowAction fires one of the panel's own actions into the pane under the stage, and
// takes that pane away when the key that opened it is pressed again.
//
// The pane is opened here rather than by the script, since the panel is what knows how its
// window is laid out. Its id is recorded on tmux for the script to read, the way the client
// viewing the popup is, because an action is a process of its own and cannot be handed
// anything else.
func (m model) runBelowAction(name, summary string) (tea.Model, tea.Cmd) {
	if m.tree.below == belowAction && m.tree.belowAction == name {
		return m.closeBelow(), nil
	}

	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	pane := m.stage.OpenSecondary()
	if pane == "" {
		return m, nil
	}

	m.tmux.SetPanelGlobal(tmux.SecondaryPaneOption, pane)
	m.stage.SecondaryClaimed()
	m.tree.below = belowAction
	m.tree.belowAction = name

	agentID := item.agent.ID

	return m, m.writeCmd(summary, func(a api) error { return a.runAction(agentID, name) })
}

// closeBelow takes the pane under the stage away, whatever was in it. The recorded pane is
// cleared with it, since tmux hands ids out again and an action reading a stale one would
// respawn whatever holds it now.
func (m model) closeBelow() model {
	m.stage.CloseSecondary()
	m.forgetBelow()

	return m
}

// forgetBelow drops what the panel remembers about the pane under the stage.
func (m *model) forgetBelow() {
	m.tmux.SetPanelGlobal(tmux.SecondaryPaneOption, "")
	m.tree.below = belowNothing
	m.tree.belowAction = ""
}

// toggleHiddenAgents draws the hidden agents alongside the rest, or puts them away again.
// Rows appear and vanish above the cursor, so it follows the row rather than the index.
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

// markSelection steps every chosen agent onto the same mark. The selection stays up
// afterwards, unlike the keys that take rows off the screen, since the cycle only means
// anything if the next press still has the same agents to move.
func (m model) markSelection() (tea.Model, tea.Cmd) {
	ids := m.tree.selected.ids
	next := nextSelectionMark(selectionMarks(m.tree.selected, m.allAgentRows()), m.marks)

	return m, m.writeCmd(
		fmt.Sprintf("Could not mark %d agents", len(ids)),
		func(a api) error { return a.setMarks(ids, next) },
	)
}

// hideAgent takes the hovered agent out of the list, or puts a revealed one back. One key
// does both directions, so the failure message comes off the row.
func (m model) hideAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	failure := fmt.Sprintf("Could not hide agent %q", agent.Name)
	if agent.Hidden {
		failure = fmt.Sprintf("Could not unhide agent %q", agent.Name)
	}

	return m, m.writeCmd(failure, func(a api) error { return a.setHidden(agent, !agent.Hidden) })
}

// stageSelection updates the stage to reflect what the cursor is on. A live agent's pane is
// swapped in, a dormant agent and a workflow header show rendered markdown, and anything
// else blanks it. The stage follows the cursor and nothing else, so what the user opened
// under it is drawn separately.
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

// stageBelow draws whatever the user opened under the stage. Only the record follows the
// cursor, since it is about the row that is hovered now rather than the one it was opened
// on. An action's pane is left alone, since what a script put there is the script's.
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
	case belowNothing, belowAction:
	}
}

// reconcileBelow forgets the pane under the stage once whatever was running in it has
// exited, since tmux takes the pane away with it and the key that opened it would otherwise
// read as closing something that is already gone.
func (m *model) reconcileBelow() {
	if m.tree.below == belowNothing || m.stage.IsSecondaryAlive() {
		return
	}

	m.forgetBelow()
}

// nextMark is where the mark key moves an agent, stepping through the configured marks and
// past the last one back to unmarked. A mark that is not configured clears.
func nextMark(current *string, marks []appconfig.Mark) *string {
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

// nextSelectionMark is where the mark key moves a whole selection, which carries no one
// mark to step from. Agents already agreeing step on together, and agents that disagree
// collapse onto the first mark, which leaves them agreeing for every press after.
//
// Collapsing onto the mark most of them hold would read the same on screen and answer
// differently, since which one that is cannot be seen without counting glyphs.
func nextSelectionMark(current []*string, marks []appconfig.Mark) *string {
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

// selectionMarks are the marks the chosen agents carry, one per agent, a nil standing for
// an unmarked one.
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

// scheduleName is the display name of the schedule that launched an agent, empty for one a
// person started. It reads the list the panel already polls, so a schedule deleted since
// shows nothing rather than a stale name.
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

// agentsView renders the nav list, the panel's own pane. The hovered resource shows in the
// stage pane rather than here.
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
			state = agentstore.StateIdle // nothing runs in a pane that is gone
		}

		name := m.filter.highlight(clipName(item.agent.Name), nameStyle(hovered, !item.alive))
		icon := lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
		rows = append(rows, prefix+"  "+icon+" "+name+scheduledMark(item.agent)+hiddenMark(item.agent.Hidden)+markGlyph(item.agent, m.marks))
	}

	return header + renderList(rows, m.tree.cursor, m.listLines(header, help)) + help
}

// openNewAgent opens the new-agent form, seeded from the given workflow's blueprint, and
// records that workflow so the created agent lands in it. A nil workflow means there are
// none, which is reported rather than opening a form whose submit could go nowhere.
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
	// The form writes into a value the panel handed it, so a submit without one is a
	// submit for a form that was never opened.
	if m.values.rename == nil {
		return m.closeForm()
	}

	target := m.target.renameAgent
	name := strings.TrimSpace(*m.values.rename)
	cmd := m.writeCmd(
		fmt.Sprintf("Could not rename agent %q", target.Name),
		func(a api) error { return a.renameAgent(target, name) },
	)
	// The row the cursor is on is about to be listed under a different name, so the
	// cursor follows the name rather than being left to clamp.
	m.tree.focus = name

	return m, tea.Batch(cmd, m.holdForm())
}

func (m model) submitAgent() (tea.Model, tea.Cmd) {
	// The form is only reachable from inside a workflow, so this holds one by the time a
	// submit happens. It is checked rather than dereferenced because saying so here costs
	// a round trip less than the refusal from the service.
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

	// A launch failure leaves the list unchanged, which reads the same as a form that
	// never submitted, so it has to be reported.
	cmd := m.writeCmd("Could not start the agent", func(a api) error { return a.createAgent(params) })

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
