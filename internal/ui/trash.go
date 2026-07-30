package ui

import (
	"fmt"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// trashState is the trash screen. The deleted agents are their own listing rather than
// a filter over the tree's, which holds the live agents only.
type trashState struct {
	agents []agentstore.Agent
	cursor int
}

// enterTrash switches to the trash, the deleted agents. It is its own screen
// rather than a section of the agents tree because a deleted agent is not
// something to act on alongside live work.
func (m model) enterTrash() (tea.Model, tea.Cmd) {
	m.screen = screenTrash
	m.stageTrash()

	return m, m.refreshCmd()
}

// applyTrash takes a freshly-listed set as the on-screen trash, keeping the
// cursor on the agent it was on rather than on its index, so a row removed above
// it does not move the selection onto something else.
func (m *model) applyTrash(fresh []agentstore.Agent) {
	selected := ""
	if agent, ok := m.selectedTrash(); ok {
		selected = agent.ID
	}

	m.trash.agents = fresh

	for i, agent := range m.trash.agents {
		if agent.ID == selected {
			m.trash.cursor = i

			return
		}
	}

	m.trash.cursor = clampCursor(m.trash.cursor, len(m.trash.agents))
}

// updateTrash drives the trash. Nav moves the selection and restages, restore
// puts the hovered agent back in the panel, and delete asks before destroying it.
func (m model) updateTrash(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case bound(k, m.keys.Trash.Quit):
		return m, m.hide()
	case bound(k, m.keys.Trash.Back):
		return m.returnToAgents()
	case bound(k, m.keys.Nav.Up):
		if m.trash.cursor > 0 {
			m.trash.cursor--
			m.stageTrash()
		}
	case bound(k, m.keys.Nav.Down):
		if m.trash.cursor < len(m.trash.agents)-1 {
			m.trash.cursor++
			m.stageTrash()
		}
	case bound(k, m.keys.Trash.Restore):
		return m.restoreFromTrash()
	case bound(k, m.keys.Trash.Delete):
		return m.deleteFromTrashConfirm()
	}

	return m, nil
}

// selectedTrash returns the deleted agent under the cursor, or false when the
// trash is empty.
func (m model) selectedTrash() (agentstore.Agent, bool) {
	if m.trash.cursor < 0 || m.trash.cursor >= len(m.trash.agents) {
		return agentstore.Agent{}, false
	}

	return m.trash.agents[m.trash.cursor], true
}

// restoreFromTrash brings the hovered agent back. Restoring relaunches the CLI on
// the same conversation, so the agent returns running rather than as a row, and
// it may come back renamed if a live agent has taken its name. It leaves the
// trash either way, so the list is re-read.
func (m model) restoreFromTrash() (tea.Model, tea.Cmd) {
	agent, ok := m.selectedTrash()
	if !ok {
		return m, nil
	}

	return m, m.restoreCmd(fmt.Sprintf("Could not restore agent %q", agent.Name), agent.ID, screenTrash)
}

// deleteFromTrashConfirm opens the prompt for destroying the hovered agent. This
// is the one action in the panel nothing undoes, so it is the only per-row
// action that asks first.
func (m model) deleteFromTrashConfirm() (tea.Model, tea.Cmd) {
	agent, ok := m.selectedTrash()
	if !ok {
		return m, nil
	}

	m.target.deleteAgent = &agent
	m.form, m.values.confirmed = deleteAgentForm(agent.Name, m.keys.Form)
	m.screen = screenTrashDelete

	return m, m.form.Init()
}

// updateTrashDelete drives the delete prompt. Confirming destroys the agent,
// declining and aborting leave it in the trash, and all three return to the
// trash rather than to the agents list, since that is where the key was pressed.
// Re-entering re-reads the list, so a destroyed agent is gone from it.
func (m model) updateTrashDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		var write tea.Cmd
		if target := m.target.deleteAgent; target != nil && m.wasConfirmed() {
			write = m.writeCmd(
				fmt.Sprintf("Could not delete agent %q", target.Name),
				func(a api) error { return a.deleteAgent(target.ID) },
			)
		}
		m.target.deleteAgent = nil
		m.values.confirmed = nil
		entered, refresh := m.enterTrash()

		return entered, tea.Batch(write, refresh)
	case huh.StateAborted:
		m.target.deleteAgent = nil
		m.values.confirmed = nil
		return m.enterTrash()
	default:
		return m, cmd
	}
}

// stageTrash shows the hovered agent's stored record in the stage. Nothing in
// the trash is running, so there is never a pane to swap in.
func (m model) stageTrash() {
	agent, ok := m.selectedTrash()
	if !ok {
		m.stage.Text(emptyTrashMarkdown)
		return
	}

	m.stage.Text(trashMarkdown(agent, m.scheduleName(agent)))
}

// trashView renders the trash's own pane, the list of deleted agents.
func (m model) trashView() string {
	header := headerStyle.Render("Trash") + "\n\n"
	hints := "\n" + m.trashHints()

	if len(m.trash.agents) == 0 {
		return header + dimStyle.Render("nothing deleted") + "\n" + hints
	}

	rows := make([]string, 0, len(m.trash.agents))
	for i, agent := range m.trash.agents {
		prefix := "  "
		name := dimStyle.Render(agent.Name)
		if i == m.trash.cursor {
			prefix = cursorStyle.Render("› ")
			name = cursorStyle.Render(agent.Name)
		}

		rows = append(rows, prefix+name+scheduledMark(agent)+markGlyph(agent, m.marks))
	}

	return header + renderList(rows, m.trash.cursor, m.listLines(header, hints)) + hints
}
