package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

type agentEdit struct {
	agent    panel.Agent
	pausing  bool
	ourPause bool
	pane     string
}

func (a *agentsScreen) isEditorOpen() bool {
	return a.editing != nil && a.editing.pane != ""
}

func (a *agentsScreen) pressEdit(m *model) tea.Cmd {
	if a.editing != nil {
		if a.editing.pausing {
			return nil
		}

		cmd := a.closeEdit(m)
		m.stageSelection()

		return cmd
	}

	if m.isScratchOpen() {
		m.reportBrief("could not open the editor", "close the scratch file first")

		return nil
	}

	item, ok := a.selectedAgent(m)
	if !ok {
		return nil
	}

	agent := item.Agent
	if agent.Cwd == "" || agent.WorktreeRemoved {
		m.reportBrief("could not edit "+agent.Name, "it has no worktree to open")

		return nil
	}

	if item.Alive && agent.State == panel.StateUncertain {
		m.reportBrief("could not edit "+agent.Name,
			"komrad cannot read its pane, so it may be writing in this worktree")

		return nil
	}

	if agent.HeldAt != nil {
		m.reportBrief("could not edit "+agent.Name, stillPaused)

		return nil
	}

	a.editing = &agentEdit{agent: agent}
	if !item.Alive {
		return a.openEditor(m)
	}

	a.editing.pausing = true

	return holdAgentCmd(m, agent.ID, agent.Name)
}

func (a *agentsScreen) applyPaused(m *model, msg reviewPausedMsg) tea.Cmd {
	if !a.isPausing(msg.agentID) {
		return nil
	}

	a.editing.pausing = false

	if msg.err != nil {
		m.report("Could not pause "+a.editing.agent.Name, msg.err)
		a.editing = nil

		return nil
	}

	a.editing.ourPause = true

	return a.openEditor(m)
}

func (a *agentsScreen) isPausing(agentID string) bool {
	return a.editing != nil && a.editing.pausing && a.editing.agent.ID == agentID
}

func (a *agentsScreen) openEditor(m *model) tea.Cmd {
	pane := m.stageEditor(dirEditorContent(m.settings.Editor, a.editing.agent.Cwd))
	if pane != "" {
		a.editing.pane = pane

		return nil
	}

	edit := *a.editing
	a.editing = nil
	m.reportBrief("could not edit "+edit.agent.Name, "there is no room beside the sidebar for an editor")
	m.stageSelection()

	if !edit.ourPause {
		return nil
	}

	return m.releaseCmd("Could not let "+edit.agent.Name+" go", edit.agent, abandonedSaid)
}

func (a *agentsScreen) closeEdit(m *model) tea.Cmd {
	if a.editing == nil {
		return nil
	}

	edit := *a.editing
	a.editing = nil

	if edit.pane == "" {
		return nil
	}

	m.unstageEditor()

	if !edit.ourPause {
		return nil
	}

	return m.releaseCmd("Could not let "+edit.agent.Name+" go", edit.agent, editedSaid)
}

func (a *agentsScreen) reconcileEditor(m *model) tea.Cmd {
	if !a.isEditorOpen() || m.isPaneAlive(a.editing.pane) {
		return nil
	}

	cmd := a.closeEdit(m)
	m.stageSelection()

	return cmd
}

func (a *agentsScreen) leave(m *model) tea.Cmd {
	a.renaming = nil

	return a.closeEdit(m)
}

func (a *agentsScreen) isEditable(m *model) bool {
	return a.editing != nil || a.isHoveringAgent(m)
}

func (a *agentsScreen) editDesc(*model) string {
	if a.editing != nil {
		return "close the editor"
	}

	return "edit worktree"
}
