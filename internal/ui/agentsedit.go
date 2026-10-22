package ui

import (
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

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

// releaseEdit closes the open editor and lets its agent go, unless the agent is still pausing.
func (a *agentsScreen) releaseEdit(m *model) screen.Request {
	if a.editing == nil || a.editing.pausing {
		return nil
	}

	return screen.Batch{a.closeEdit(m), screen.Restage{}}
}

// pressEdit opens an editor on the agent's worktree, or closes the one already open.
func (a *agentsScreen) pressEdit(m *model, item panel.AgentRow) screen.Request {
	if a.editing != nil {
		return a.releaseEdit(m)
	}

	agent := item.Agent
	if agent.Cwd == "" || m.listing.Tasks.FindForAgent(agent).IsWorktreeRemoved() {
		return screen.Note{Kind: screen.Error, Summary: "could not edit " + agent.Name, Detail: "it has no worktree to open"}
	}

	if item.Alive && agent.State == panel.StateUncertain {
		return screen.Note{Kind: screen.Error, Summary: "could not edit " + agent.Name, Detail: "komrad cannot read its pane, so it may be writing in this worktree"}
	}

	if agent.HeldAt != nil {
		return screen.Note{Kind: screen.Error, Summary: "could not edit " + agent.Name, Detail: stillPaused}
	}

	if reason, refused := m.isRemoteEditRefused(); refused {
		return screen.Note{Kind: screen.Error, Summary: "could not edit " + agent.Name, Detail: reason}
	}

	a.editing = &agentEdit{agent: agent}
	if !item.Alive {
		return a.openEditor(m)
	}

	a.editing.pausing = true

	return holdAgentCmd(m, agent.ID, agent.Name)
}

func (a *agentsScreen) applyPaused(m *model, msg reviewPausedMsg) screen.Request {
	if !a.isPausing(msg.agentID) {
		return nil
	}

	a.editing.pausing = false

	if msg.err != nil {
		refused := screen.Report{Summary: "Could not pause " + a.editing.agent.Name, Err: msg.err}
		a.editing = nil

		return refused
	}

	a.editing.ourPause = true

	return a.openEditor(m)
}

func (a *agentsScreen) isPausing(agentID string) bool {
	return a.editing != nil && a.editing.pausing && a.editing.agent.ID == agentID
}

// agentEditorStaged is the pane an editor over an agent's directory was given, empty where the
// split was refused.
type agentEditorStaged struct {
	pane string
}

func (a *agentsScreen) openEditor(m *model) screen.Request {
	return screen.Stage{
		Content: m.agentDirEditorContent(a.editing.agent.Cwd),
		Staged:  func(pane string) tea.Msg { return agentEditorStaged{pane: pane} },
	}
}

// applyEditorStaged keeps the editor's pane, or gives the edit up when there was no room for it.
func (a *agentsScreen) applyEditorStaged(m *model, staged agentEditorStaged) screen.Request {
	if a.editing == nil {
		return nil
	}

	if staged.pane != "" {
		a.editing.pane = staged.pane

		return nil
	}

	edit := *a.editing
	a.editing = nil
	refused := screen.Batch{
		screen.Note{Kind: screen.Error, Summary: "could not edit " + edit.agent.Name, Detail: "there is no room beside the sidebar for an editor"},
		screen.Restage{},
	}
	if !edit.ourPause {
		return refused
	}

	return append(refused, m.releaseWrite("Could not let "+edit.agent.Name+" go", edit.agent, abandonedSaid))
}

func (a *agentsScreen) closeEdit(m *model) screen.Request {
	if a.editing == nil {
		return nil
	}

	edit := *a.editing
	a.editing = nil

	if edit.pane == "" {
		return nil
	}

	if !edit.ourPause {
		return screen.Unstage{}
	}

	return screen.Batch{screen.Unstage{}, m.releaseWrite("Could not let "+edit.agent.Name+" go", edit.agent, editedSaid)}
}

func (a *agentsScreen) reconcileEditor(m *model) screen.Request {
	if !a.isEditorOpen() || m.isPaneAlive(a.editing.pane) {
		return nil
	}

	return screen.Batch{a.closeEdit(m), screen.Restage{}}
}

func (a *agentsScreen) leaveScreen(m *model) screen.Request {
	a.renaming = nil

	return a.closeEdit(m)
}
