package ui

import (
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

type agentEdit struct {
	agent panel.Agent
	pane  string
}

func (a *agentsScreen) isEditorOpen() bool {
	return a.editing != nil && a.editing.pane != ""
}

// closeEdit closes the open editor and tells its agent so.
func (a *agentsScreen) closeEdit() screen.Request {
	if a.editing == nil {
		return nil
	}

	return screen.Batch{a.dropEdit(), screen.Restage{}}
}

// pressEdit opens an editor on the agent's worktree, or closes the one already open.
func (a *agentsScreen) pressEdit(in screen.Inputs, item panel.AgentRow) screen.Request {
	if a.editing != nil {
		return a.closeEdit()
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

	if item.Alive {
		switch agent.State {
		case panel.StateThinking:
			return refuse("it is writing in this worktree, so stop it before you edit")
		case panel.StateUncertain:
			return refuse("komrad cannot read its pane, so it may be writing in this worktree")
		case panel.StateWaiting, panel.StateIdle, panel.StateErrored, panel.StateSettingUp,
			panel.StateFailedToCreate, panel.StateFailedToCleanup:
		}
	}

	if reason, refused := a.deps.editorSettings.IsRemoteEditRefused(); refused {
		return refuse(reason)
	}

	a.editing = &agentEdit{agent: agent}

	return a.openEditor()
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
func (a *agentsScreen) applyEditorStaged(staged agentEditorStaged) screen.Request {
	if a.editing == nil {
		return nil
	}

	if staged.pane != "" {
		a.editing.pane = staged.pane

		return nil
	}

	edit := *a.editing
	a.editing = nil

	return screen.Batch{
		screen.Note{
			Kind:    screen.Error,
			Summary: "could not edit " + edit.agent.Name,
			Detail:  "there is no room beside the sidebar for an editor",
		},
		screen.Restage{},
	}
}

func (a *agentsScreen) dropEdit() screen.Request {
	if a.editing == nil {
		return nil
	}

	edit := *a.editing
	a.editing = nil

	if edit.pane == "" {
		return nil
	}

	return screen.Batch{screen.Unstage{}, a.deps.clients.EditedWrite(edit.agent, nil)}
}

func (a *agentsScreen) ReconcileEditor(in screen.Inputs) screen.Request {
	if !a.isEditorOpen() || a.deps.panes.IsPaneAlive(a.editing.pane) {
		return nil
	}

	return screen.Batch{a.dropEdit(), screen.Restage{}}
}

func (a *agentsScreen) LeaveScreen() screen.Request {
	a.renaming = nil

	return a.dropEdit()
}
