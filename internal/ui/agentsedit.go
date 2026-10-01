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

func (a *agentsScreen) pressEdit(h host, s panelShared) tea.Cmd {
	if a.editing != nil {
		if a.editing.pausing {
			return nil
		}

		cmd := a.closeEdit(h)
		h.stageSelection()

		return cmd
	}

	if h.isScratchOpen() {
		h.reportBrief("could not open the editor", "close the scratch file first")

		return nil
	}

	item, ok := a.selectedAgent(s)
	if !ok {
		return nil
	}

	agent := item.Agent
	if agent.Cwd == "" || agent.WorktreeRemoved {
		h.reportBrief("could not edit "+agent.Name, "it has no worktree to open")

		return nil
	}

	if item.Alive && agent.State == panel.StateUncertain {
		h.reportBrief("could not edit "+agent.Name,
			"komrad cannot read its pane, so it may be writing in this worktree")

		return nil
	}

	if agent.HeldAt != nil {
		h.reportBrief("could not edit "+agent.Name, stillPaused)

		return nil
	}

	a.editing = &agentEdit{agent: agent}
	if !item.Alive {
		return a.openEditor(h, s)
	}

	a.editing.pausing = true

	return holdAgentCmd(h, s, agent.ID, agent.Name)
}

func (a *agentsScreen) applyPaused(h host, s panelShared, msg reviewPausedMsg) tea.Cmd {
	if !a.isPausing(msg.agentID) {
		return nil
	}

	a.editing.pausing = false

	if msg.err != nil {
		h.report("Could not pause "+a.editing.agent.Name, msg.err)
		a.editing = nil

		return nil
	}

	a.editing.ourPause = true

	return a.openEditor(h, s)
}

func (a *agentsScreen) isPausing(agentID string) bool {
	return a.editing != nil && a.editing.pausing && a.editing.agent.ID == agentID
}

func (a *agentsScreen) openEditor(h host, s panelShared) tea.Cmd {
	pane := h.stageEditor(dirEditorContent(s.editor, a.editing.agent.Cwd))
	if pane != "" {
		a.editing.pane = pane

		return nil
	}

	edit := *a.editing
	a.editing = nil
	h.reportBrief("could not edit "+edit.agent.Name, "there is no room beside the sidebar for an editor")
	h.stageSelection()

	if !edit.ourPause {
		return nil
	}

	return h.releaseCmd("Could not let "+edit.agent.Name+" go", edit.agent, abandonedSaid)
}

func (a *agentsScreen) closeEdit(h host) tea.Cmd {
	if a.editing == nil {
		return nil
	}

	edit := *a.editing
	a.editing = nil

	if edit.pane == "" {
		return nil
	}

	h.unstageEditor()

	if !edit.ourPause {
		return nil
	}

	return h.releaseCmd("Could not let "+edit.agent.Name+" go", edit.agent, editedSaid)
}

func (a *agentsScreen) reconcileEditor(h host) tea.Cmd {
	if !a.isEditorOpen() || h.isPaneAlive(a.editing.pane) {
		return nil
	}

	cmd := a.closeEdit(h)
	h.stageSelection()

	return cmd
}

func (a *agentsScreen) leave(h host) tea.Cmd {
	a.renaming = nil

	return a.closeEdit(h)
}

func (a *agentsScreen) isEditable(s panelShared) bool {
	return a.editing != nil || a.isHoveringAgent(s)
}

func (a *agentsScreen) editDesc(panelShared) string {
	if a.editing != nil {
		return "close the editor"
	}

	return "edit worktree"
}
