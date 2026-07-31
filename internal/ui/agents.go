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

// updateAgents drives the nav list. The nav keys move the selection and restage,
// and letter keys act on the hovered agent or workflow.
func (m model) updateAgents(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// These act on the whole view rather than the row under the cursor, so they are
	// checked before anything that reads the cursor and never collide with the per-row
	// keys below.
	if bound(k, m.keys.Workflows.New) {
		return m.newWorkflowForm()
	}
	if bound(k, m.keys.Agents.Search) {
		return m.enterSearch()
	}
	if bound(k, m.keys.Schedules.Open) {
		return m.enterSchedules()
	}
	if bound(k, m.keys.Trash.Open) {
		return m.enterTrash()
	}
	if bound(k, m.keys.Agents.ShowHidden) {
		return m.toggleHiddenAgents()
	}

	// On a workflow header the workflow bindings act on that folder's workflow.
	if workflow, ok := m.hoveredWorkflow(); ok {
		return m.updateHoveredWorkflow(key, workflow)
	}

	switch {
	case bound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case bound(k, m.keys.Agents.Back):
		return m, m.hide()
	case bound(k, m.keys.Agents.New):
		return m.openNewAgent(m.enclosingWorkflow())
	case bound(k, m.keys.Nav.Up):
		if m.tree.cursor > 0 {
			m.tree.cursor--
			m.tree.infoPinned = false
			m.stageSelection()
		}
	case bound(k, m.keys.Nav.Down):
		if m.tree.cursor < len(m.agentRows())-1 {
			m.tree.cursor++
			m.tree.infoPinned = false
			m.stageSelection()
		}
	case bound(k, m.keys.Agents.Info):
		if _, ok := m.selectedAgent(); !ok {
			return m, nil
		}
		m.tree.infoPinned = !m.tree.infoPinned
		m.stageSelection()
	case bound(k, m.keys.Agents.Open):
		return m.openAgent()
	case bound(k, m.keys.Agents.Mark):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		return m.markAgent(item.agent)
	case bound(k, m.keys.Agents.Hide):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		return m.hideAgent(item.agent)
	case bound(k, m.keys.Agents.Rename):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		m.target.renameAgent = item.agent
		m.form, m.values.rename = renameForm(item.agent.Name, m.keys.Form)
		m.screen = screenRename
		return m, m.form.Init()
	case bound(k, m.keys.Agents.Delete):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		// Killing a pane while it sits in the stage slot would take the stage down with it.
		m.stage.Release(item.agent.Pane)

		return m, m.writeCmd(
			fmt.Sprintf("Could not delete agent %q", item.agent.Name),
			func(a api) error { return a.archiveAgent(item.agent) },
		)
	}

	return m, nil
}

// openAgent acts on the hovered agent. A dormant agent is restored and staged live. A
// live agent is already staged from hovering, so opening it only unpins the info view to
// bring the agent back.
func (m model) openAgent() (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}
	if item.alive {
		if m.tree.infoPinned {
			m.tree.infoPinned = false
			m.stageSelection()
		}
		return m, nil
	}

	return m, m.restoreCmd(
		fmt.Sprintf("Could not restore agent %q", item.agent.Name),
		item.agent.ID,
		screenAgents,
	)
}

// toggleHiddenAgents draws the hidden agents alongside the rest, or puts them away again.
// Rows appear and vanish above the cursor either way, so it follows the agent it was on
// rather than the index it held.
func (m model) toggleHiddenAgents() (tea.Model, tea.Cmd) {
	selected := ""
	if item, ok := m.selectedAgent(); ok {
		selected = item.agent.Name
	}

	m.tree.showHidden = !m.tree.showHidden

	rows := m.agentRows()
	if idx := rowIndexOfAgentName(rows, selected); idx >= 0 {
		m.tree.cursor = idx
	} else {
		m.tree.cursor = clampCursor(m.tree.cursor, len(rows))
	}
	m.stageSelection()

	return m, nil
}

func (m model) markAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	next := nextMark(agent.Mark, m.marks)

	return m, m.writeCmd(
		fmt.Sprintf("Could not mark agent %q", agent.Name),
		func(a api) error { return a.setMark(agent, next) },
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

// stageSelection updates the stage pane to reflect what the cursor is on. A live
// agent's pane is swapped in, a dormant agent and a workflow header show rendered
// markdown, and anything else blanks the stage. With info pinned, an agent shows
// its stored record instead of itself.
func (m model) stageSelection() {
	rows := m.agentRows()
	if m.tree.cursor < 0 || m.tree.cursor >= len(rows) {
		m.stage.Blank()
		return
	}

	r := rows[m.tree.cursor]
	switch {
	case r.header && r.workflowIndex >= 0:
		m.stage.Text(workflowMarkdown(m.tree.workflows[r.workflowIndex], m.agentCount(m.tree.workflows[r.workflowIndex].ID)))
	case r.header:
		m.stage.Blank()
	case m.tree.infoPinned:
		m.stage.Text(agentMarkdown(r.item.agent, r.item.alive, m.scheduleName(r.item.agent)))
	case r.item.alive:
		m.stage.Agent(r.item.agent.Pane)
	default:
		m.stage.Text(agentMarkdown(r.item.agent, false, m.scheduleName(r.item.agent)))
	}
}

// nextMark is where the mark key moves an agent, stepping through the configured marks and
// past the last one back to unmarked. A mark that is not configured clears, so the key is
// always a way back out of one.
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

// scheduleName is the display name of the schedule that launched an agent, or empty for
// one a person started. It reads from the list the panel already polls, so a schedule
// deleted since resolves to nothing rather than to a stale name.
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

// agentsView renders the nav list, the panel's own pane. The hovered resource is
// shown in the separate stage pane, not here.
func (m model) agentsView() string {
	header := headerStyle.Render("Agents and Workflows") + "\n\n"
	hints := "\n" + m.actionHints()

	all := m.agentRows()
	rows := make([]string, 0, len(all))
	for i, r := range all {
		selected := i == m.tree.cursor
		prefix := "  "
		if selected {
			prefix = cursorStyle.Render("› ")
		}

		if r.header {
			label := r.label
			if selected {
				label = cursorStyle.Render(label)
			}
			rows = append(rows, prefix+dimStyle.Render("⧉")+" "+label+hiddenTally(r.hiddenCount, m.tree.showHidden))
			continue
		}

		item := r.item
		name := item.agent.Name
		if selected {
			name = cursorStyle.Render(name)
		}

		state := item.agent.State
		if !item.alive {
			state = agentstore.StateIdle // nothing runs in a pane that is gone
			name = dimStyle.Render(item.agent.Name)
			if selected {
				name = cursorStyle.Render(item.agent.Name)
			}
		}

		icon := lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
		rows = append(rows, prefix+"  "+icon+" "+name+scheduledMark(item.agent)+hiddenMark(item.agent.Hidden)+markGlyph(item.agent, m.marks))
	}

	return header + renderList(rows, m.tree.cursor, m.listLines(header, hints)) + hints
}

// errNoWorkflow is what the panel reports when a create is attempted with no
// workflow to put the agent in, which only happens when none exist at all.
var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

// openNewAgent opens the new-agent form, seeded from the given workflow's blueprint, and
// records that workflow so the created agent is placed in it. A nil workflow means there
// are none, which is reported rather than opening a form whose submit could not go
// anywhere.
func (m model) openNewAgent(workflow *workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if workflow == nil {
		m.report("Could not start an agent", errNoWorkflow)

		return m, nil
	}

	m.target.newAgentWorkflow = workflow
	m.form, m.values.agent = newAgentForm(m.keys.Form, workflow.Cwd, workflow.Flags)
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
		name := ""
		if m.values.rename != nil {
			name = strings.TrimSpace(*m.values.rename)
		}
		target := m.target.renameAgent
		cmd := m.writeCmd(
			fmt.Sprintf("Could not rename agent %q", target.Name),
			func(a api) error { return a.renameAgent(target, name) },
		)
		// The row the cursor is on is about to be listed under a different name, so the
		// cursor follows the name rather than being left to clamp.
		m.tree.focus = name
		m.values.rename = nil
		m.screen = screenAgents

		return m, cmd
	case huh.StateAborted:
		m.values.rename = nil
		m.screen = screenAgents
		m.stageSelection()
		return m, nil
	default:
		return m, cmd
	}
}

func (m model) submitAgent() (tea.Model, tea.Cmd) {
	// The form is only reachable from inside a workflow, so this holds one by the time a
	// submit happens. It is checked rather than dereferenced because saying so here costs a
	// round trip less than the refusal from the service.
	if m.target.newAgentWorkflow == nil || m.values.agent == nil {
		m.report("Could not start the agent", errNoWorkflow)
		m.screen = screenAgents
		m.stageSelection()

		return m, nil
	}

	params := createAgentParams{
		Prompt:     m.values.agent.Prompt,
		Title:      m.values.agent.Title,
		Dir:        m.currentDir(m.values.agent.Dir),
		Flags:      m.values.agent.Flags,
		WorkflowID: m.target.newAgentWorkflow.ID,
	}

	// A launch failure leaves the list unchanged, and a list that comes back without the
	// new agent reads the same as a form that never submitted, so it has to be reported.
	cmd := m.writeCmd("Could not start the agent", func(a api) error { return a.createAgent(params) })
	m.target.newAgentWorkflow = nil
	m.values.agent = nil
	m.screen = screenAgents

	return m, cmd
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && bound(key.String(), m.keys.Form.Submit) {
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
		m.target.newAgentWorkflow = nil
		m.values.agent = nil
		m.screen = screenAgents
		m.tree.cursor = clampCursor(m.tree.cursor, len(m.agentRows()))
		m.stageSelection()
		return m, nil
	default:
		return m, cmd
	}
}
