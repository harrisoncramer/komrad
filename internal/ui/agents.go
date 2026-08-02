package ui

import (
	"errors"
	"fmt"
	"strings"

	"weave/internal/appconfig"

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
	// Before the workflow bindings below, since a header has a directory of its own and
	// opening a shell in it is worth as much as opening one on an agent.
	if isBound(k, m.keys.Agents.Terminal) {
		return m.openTerminal()
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
		if _, ok := m.selectedAgent(); !ok {
			return m, nil
		}
		m.tree.infoPinned = !m.tree.infoPinned
		m.stageSelection()
	case isBound(k, m.keys.Agents.Open):
		return m.openAgent()
	case isBound(k, m.keys.Agents.Editor):
		return m.openEditor()
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
		// Killing a pane while it sits in the stage slot would take the stage down with it.
		m.stage.Release(item.agent.Pane)

		return m, m.writeCmd(
			fmt.Sprintf("Could not archive agent %q", item.agent.Name),
			func(a api) error { return a.archiveAgent(item.agent) },
		)
	}

	return m, nil
}

// updateAgentsSelection is what the agents list still answers while a selection is up, past
// the selection keys its caller has already taken. Movement and the way out stay live,
// along with the mark key, which acts on the whole selection. So does the hidden toggle,
// which changes what is drawn rather than acting on a row and is the one key that can put a
// chosen agent off screen.
func (m model) updateAgentsSelection(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Agents.Mark):
		return m.markSelection()
	case isBound(k, m.keys.Agents.ShowHidden):
		return m.toggleHiddenAgents()
	case isBound(k, m.keys.Nav.Up):
		return m.moveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveCursor(1), nil
	}

	return m, nil
}

// moveCursor steps the tree cursor and restages what it lands on, doing nothing at either
// end of the list.
func (m model) moveCursor(delta int) model {
	next := m.tree.cursor + delta
	if next < 0 || next >= len(m.agentRows()) {
		return m
	}

	m.tree.cursor = next
	m.tree.unpin()
	m.stageSelection()

	return m
}

// openAgent acts on the hovered agent. A dormant one is restored and staged live, and a
// live one is already staged from hovering, so opening it only unpins whatever was over it.
func (m model) openAgent() (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}
	if item.alive {
		if m.tree.infoPinned || m.tree.logsPinned {
			m.tree.unpin()
			m.stageSelection()
		}
		return m, nil
	}

	return m, m.resumeCmd(resumeParams{
		summary: fmt.Sprintf("Could not resume agent %q", item.agent.Name),
		id:      item.agent.ID,
	})
}

// openEditor opens the hovered agent's directory in the user's editor and closes the panel
// behind it. The window is created on the user's own tmux server, since an editor is
// theirs to keep working in once the popup is gone.
//
// A failure leaves the popup up so the banner is readable.
func (m model) openEditor() (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	dir := item.agent.Cwd
	err := m.tmux.OpenWindow(m.popup.InvokingClient(), item.agent.Name, dir, editorArgv(m.editor, dir)...)
	m.report(fmt.Sprintf("Could not open %q in your editor", dir), err)

	if err != nil {
		return m, nil
	}

	return m, m.hide()
}

// openTerminal splits a shell under the stage, rooted where the hovered row runs, and
// leaves the cursor in it. It goes under the stage rather than under the list so the shell
// sits beneath the agent it is about, and the list keeps its full height.
//
// The pane is the user's from there: nothing follows the list afterwards and nothing here
// closes it, so it is a tmux split like any other.
func (m model) openTerminal() (tea.Model, tea.Cmd) {
	anchor := m.stage.SlotPane()
	if anchor == "" {
		anchor = m.stage.NavPane()
	}

	m.tmux.SplitBelow(anchor, m.hoveredDir())

	return m, nil
}

// hoveredDir is the directory the row under the cursor runs in. An empty list, or a row
// carrying no directory, falls back to where the user was when they opened the panel.
func (m model) hoveredDir() string {
	rows := m.agentRows()
	if m.tree.cursor < 0 || m.tree.cursor >= len(rows) {
		return m.currentDir("")
	}

	row := rows[m.tree.cursor]
	if row.header {
		return m.currentDir(row.workflow.Cwd)
	}

	return m.currentDir(row.item.agent.Cwd)
}

// editorArgv is what the editor window runs, split on whitespace so an editor carrying its
// own flags still works. tmux runs the result itself rather than through a shell.
//
// The directory is an argument as well as the window's working directory, since an editor
// given nothing to open lands on an empty buffer. An empty editor leaves the argv empty,
// which tmux reads as a plain shell rather than a command it cannot find.
func editorArgv(editor, dir string) []string {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return nil
	}

	return append(fields, dir)
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
	m.tree.selected = m.tree.selected.pruned(rows)
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
	next := nextSelectionMark(selectionMarks(m.tree.selected, m.agentRows()), m.marks)

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

// stageSelection updates the stage pane to reflect what the cursor is on. A live agent's
// pane is swapped in, a dormant agent and a workflow header show rendered markdown, and
// anything else blanks the stage. With info pinned, an agent shows its stored record.
func (m model) stageSelection() {
	// The log is about the service rather than a row, so it is answered before the cursor
	// is read at all, and shows from an empty list too, which is when it is worth most.
	if m.tree.logsPinned {
		m.stage.Log(m.logPath, logArgv(m.logViewer, m.logPath, m.binary))

		return
	}

	rows := m.agentRows()
	if m.tree.cursor < 0 || m.tree.cursor >= len(rows) {
		m.stage.Blank()
		return
	}

	r := rows[m.tree.cursor]
	switch {
	case r.header:
		m.stage.Text(workflowMarkdown(r.workflow, m.agentCount(r.workflow.ID)))
	case m.tree.infoPinned:
		m.stage.Text(agentMarkdown(r.item.agent, r.item.alive, m.scheduleName(r.item.agent)))
	case r.item.alive:
		m.stage.Agent(r.item.agent.Pane)
	default:
		m.stage.Text(agentMarkdown(r.item.agent, false, m.scheduleName(r.item.agent)))
	}
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
// an unmarked one. It reads the rows rather than a listing, so it sees what the user is
// looking at.
func selectionMarks(sel selection, rows []agentRow) []*string {
	marks := make([]*string, 0, sel.count())
	for _, row := range rows {
		if !row.header && sel.has(row.item.agent.ID) {
			marks = append(marks, row.item.agent.Mark)
		}
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
	header := headerStyle.Render("Agents and Workflows") + "\n\n"
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
			label := r.workflow.Name
			if hovered {
				label = cursorStyle.Render(label)
			}
			rows = append(rows, prefix+dimStyle.Render(workflowIcon)+" "+label+hiddenTally(r.hiddenCount, m.tree.showHidden))
			continue
		}

		item := r.item
		name := item.agent.Name
		if hovered {
			name = cursorStyle.Render(name)
		}

		state := item.agent.State
		if !item.alive {
			state = agentstore.StateIdle // nothing runs in a pane that is gone
			name = dimStyle.Render(item.agent.Name)
			if hovered {
				name = cursorStyle.Render(item.agent.Name)
			}
		}

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
