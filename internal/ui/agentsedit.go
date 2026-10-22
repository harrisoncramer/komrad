package ui

import (
	"komrad/internal/ui/api"
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
func (a *agentsScreen) releaseEdit(in screen.Inputs) screen.Request {
	if a.editing == nil || a.editing.pausing {
		return nil
	}

	return screen.Batch{a.closeEdit(in), screen.Restage{}}
}

// pressEdit opens an editor on the agent's worktree, or closes the one already open.
func (a *agentsScreen) pressEdit(in screen.Inputs, item panel.AgentRow) screen.Request {
	if a.editing != nil {
		return a.releaseEdit(in)
	}

	agent := item.Agent
	refuse := func(reason string) screen.Note {
		return screen.Note{
			Kind:    screen.Error,
			Summary: "could not edit " + agent.Name,
			Detail:  reason,
		}
	}

	if agent.Cwd == "" || in.Listing.Tasks.FindForAgent(agent).IsWorktreeRemoved() {
		return refuse("it has no worktree to open")
	}

	if item.Alive && agent.State == panel.StateUncertain {
		return refuse("komrad cannot read its pane, so it may be writing in this worktree")
	}

	if agent.HeldAt != nil {
		return refuse(stillPaused)
	}

	if reason, refused := a.deps.editorSettings.IsRemoteEditRefused(); refused {
		return refuse(reason)
	}

	a.editing = &agentEdit{agent: agent}
	if !item.Alive {
		return a.openEditor()
	}

	a.editing.pausing = true

	return holdAgent(a.deps.clients, agent.ID, agent.Name)
}

func (a *agentsScreen) applyPaused(msg reviewPausedMsg) screen.Request {
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

	return a.openEditor()
}

func (a *agentsScreen) isPausing(agentID string) bool {
	return a.editing != nil && a.editing.pausing && a.editing.agent.ID == agentID
}

// agentEditorStaged is the pane an editor over an agent's directory was given, empty where the
// split was refused.
type agentEditorStaged struct {
	pane string
}

func (a *agentsScreen) openEditor() screen.Request {
	return screen.Stage{
		Content: a.deps.editorSettings.DirContent(a.editing.agent.Cwd),
		Staged:  func(pane string) tea.Msg { return agentEditorStaged{pane: pane} },
	}
}

// applyEditorStaged keeps the editor's pane, or gives the edit up when there was no room for it.
func (a *agentsScreen) applyEditorStaged(in screen.Inputs, staged agentEditorStaged) screen.Request {
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
		screen.Note{
			Kind:    screen.Error,
			Summary: "could not edit " + edit.agent.Name,
			Detail:  "there is no room beside the sidebar for an editor",
		},
		screen.Restage{},
	}
	if !edit.ourPause {
		return refused
	}

	return append(refused, a.deps.clients.ReleaseWrite(api.ReleaseWriteParams{
		Summary: "Could not let " + edit.agent.Name + " go",
		Agent:   edit.agent,
		Said:    abandonedSaid,
		Edited:  in.Holds[edit.agent.ID],
	}))
}

func (a *agentsScreen) closeEdit(in screen.Inputs) screen.Request {
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

	return screen.Batch{screen.Unstage{}, a.deps.clients.ReleaseWrite(api.ReleaseWriteParams{
		Summary: "Could not let " + edit.agent.Name + " go",
		Agent:   edit.agent,
		Said:    editedSaid,
		Edited:  in.Holds[edit.agent.ID],
	})}
}

func (a *agentsScreen) reconcileEditor(m *model, in screen.Inputs) screen.Request {
	if !a.isEditorOpen() || a.deps.panes.IsPaneAlive(a.editing.pane) {
		return nil
	}

	return screen.Batch{a.closeEdit(in), screen.Restage{}}
}

func (a *agentsScreen) leaveScreen(m *model) screen.Request {
	a.renaming = nil

	return a.closeEdit(m.inputs())
}
