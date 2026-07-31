package ui

import (
	"fmt"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// archiveState is the archive screen. The archived agents are their own listing
// rather than a filter over the tree's, which holds the live agents only.
type archiveState struct {
	agents []agentstore.Agent
	cursor int
}

// enterArchive switches to the archive, the archived agents. It is its own screen
// rather than a section of the agents tree because an archived agent is not
// something to act on alongside live work.
func (m model) enterArchive() (tea.Model, tea.Cmd) {
	m.screen = screenArchive
	m.stageArchive()

	return m, m.refreshCmd()
}

// applyArchive takes a freshly-listed set as the on-screen archive, keeping the
// cursor on the agent it was on rather than on its index, so a row removed above
// it does not move the selection onto something else.
func (m *model) applyArchive(fresh []agentstore.Agent) {
	selected := ""
	if agent, ok := m.selectedArchived(); ok {
		selected = agent.ID
	}

	m.archive.agents = fresh

	for i, agent := range m.archive.agents {
		if agent.ID == selected {
			m.archive.cursor = i

			return
		}
	}

	m.archive.cursor = clampCursor(m.archive.cursor, len(m.archive.agents))
}

// updateArchive drives the archive. Nav moves the selection and restages, restore
// puts the hovered agent back in the panel, and delete asks before destroying it.
func (m model) updateArchive(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case bound(k, m.keys.Archive.Quit):
		return m, m.hide()
	case bound(k, m.keys.Archive.Back):
		return m.returnToAgents()
	case bound(k, m.keys.Nav.Up):
		if m.archive.cursor > 0 {
			m.archive.cursor--
			m.stageArchive()
		}
	case bound(k, m.keys.Nav.Down):
		if m.archive.cursor < len(m.archive.agents)-1 {
			m.archive.cursor++
			m.stageArchive()
		}
	case bound(k, m.keys.Archive.Restore):
		return m.restoreFromArchive()
	case bound(k, m.keys.Archive.Delete):
		return m.deleteFromArchiveConfirm()
	}

	return m, nil
}

// selectedArchived returns the archived agent under the cursor, or false when the
// archive is empty.
func (m model) selectedArchived() (agentstore.Agent, bool) {
	if m.archive.cursor < 0 || m.archive.cursor >= len(m.archive.agents) {
		return agentstore.Agent{}, false
	}

	return m.archive.agents[m.archive.cursor], true
}

// restoreFromArchive brings the hovered agent back. Restoring relaunches the CLI on
// the same conversation, so the agent returns running rather than as a row, and
// it may come back renamed if a live agent has taken its name. It leaves the
// archive either way, so the list is re-read.
func (m model) restoreFromArchive() (tea.Model, tea.Cmd) {
	agent, ok := m.selectedArchived()
	if !ok {
		return m, nil
	}

	return m, m.restoreCmd(fmt.Sprintf("Could not restore agent %q", agent.Name), agent.ID, screenArchive)
}

// deleteFromArchiveConfirm opens the prompt for destroying the hovered agent. This
// is the one thing in the panel nothing undoes, so it is the only per-row key
// that asks first.
func (m model) deleteFromArchiveConfirm() (tea.Model, tea.Cmd) {
	agent, ok := m.selectedArchived()
	if !ok {
		return m, nil
	}

	m.target.deleteAgent = &agent
	m.form, m.values.confirmed = deleteAgentForm(agent.Name, m.keys.Form)
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
		if target := m.target.deleteAgent; target != nil && m.wasConfirmed() {
			write = m.writeCmd(
				fmt.Sprintf("Could not delete agent %q", target.Name),
				func(a api) error { return a.deleteAgent(target.ID) },
			)
		}
		m.target.deleteAgent = nil
		m.values.confirmed = nil
		entered, refresh := m.enterArchive()

		return entered, tea.Batch(write, refresh)
	case huh.StateAborted:
		m.target.deleteAgent = nil
		m.values.confirmed = nil
		return m.enterArchive()
	default:
		return m, cmd
	}
}

// stageArchive shows the hovered agent's stored record in the stage. Nothing in
// the archive is running, so there is never a pane to swap in.
func (m model) stageArchive() {
	agent, ok := m.selectedArchived()
	if !ok {
		m.stage.Text(emptyArchiveMarkdown)
		return
	}

	m.stage.Text(archiveMarkdown(agent, m.scheduleName(agent)))
}

// archiveView renders the archive's own pane, the list of archived agents.
func (m model) archiveView() string {
	header := headerStyle.Render("Archive") + "\n\n"
	hints := "\n" + m.archiveHints()

	if len(m.archive.agents) == 0 {
		return header + dimStyle.Render("nothing archived") + "\n" + hints
	}

	rows := make([]string, 0, len(m.archive.agents))
	for i, agent := range m.archive.agents {
		prefix := "  "
		name := dimStyle.Render(agent.Name)
		if i == m.archive.cursor {
			prefix = cursorStyle.Render("› ")
			name = cursorStyle.Render(agent.Name)
		}

		rows = append(rows, prefix+name+scheduledMark(agent)+markGlyph(agent, m.marks))
	}

	return header + renderList(rows, m.archive.cursor, m.listLines(header, hints)) + hints
}
