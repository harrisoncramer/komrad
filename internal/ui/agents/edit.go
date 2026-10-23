package agents

import (
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

type agentEdit struct {
	agent panel.Agent
	pane  string
}

// IsEditorOpen reports whether an editor on an agent's notes is open on the stage.
func (a *Screen) IsEditorOpen() bool {
	return a.editing != nil && a.editing.pane != ""
}

// closeEdit closes the open editor and tells its agent so.
func (a *Screen) closeEdit() screen.Request {
	if a.editing == nil {
		return nil
	}

	return screen.Batch{a.dropEdit(), screen.RefreshStage{}}
}

// pressEdit opens an editor on the agent's worktree, or closes the one already open.
func (a *Screen) pressEdit(in screen.Inputs, item panel.AgentRow) screen.Request {
	if a.editing != nil {
		return a.closeEdit()
	}

	agent := item.Agent
	refuse := func(reason string) screen.PostMessage {
		return screen.PostMessage{
			MessageKind: screen.Error,
			Summary:     "could not edit " + agent.Name,
			Detail:      reason,
		}
	}

	if agent.Cwd == "" || in.Listing.Tasks.FindForAgent(agent).IsWorktreeRemoved() {
		return refuse("it has no worktree to open")
	}

	if item.Alive {
		switch agent.State {
		case agentv1.AgentState_AGENT_STATE_THINKING:
			return refuse("it is writing in this worktree, so stop it before you edit")
		case agentv1.AgentState_AGENT_STATE_UNCERTAIN:
			return refuse("komrad cannot read its pane, so it may be writing in this worktree")
		case agentv1.AgentState_AGENT_STATE_WAITING, agentv1.AgentState_AGENT_STATE_IDLE, agentv1.AgentState_AGENT_STATE_ERRORED, agentv1.AgentState_AGENT_STATE_SETTING_UP,
			agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE, agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP, agentv1.AgentState_AGENT_STATE_UNSPECIFIED:
		}
	}

	if reason, refused := a.deps.EditorSettings.IsRemoteEditRefused(); refused {
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

func (a *Screen) openEditor() screen.Request {
	return screen.Stage{
		Content: a.deps.EditorSettings.DirContent(a.editing.agent.Cwd),
		Staged:  func(pane string) tea.Msg { return agentEditorStaged{pane: pane} },
	}
}

// applyEditorStaged keeps the editor's pane, or gives the edit up when there was no room for it.
func (a *Screen) applyEditorStaged(staged agentEditorStaged) screen.Request {
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
		screen.PostMessage{
			MessageKind: screen.Error,
			Summary:     "could not edit " + edit.agent.Name,
			Detail:      "there is no room beside the sidebar for an editor",
		},
		screen.RefreshStage{},
	}
}

func (a *Screen) dropEdit() screen.Request {
	if a.editing == nil {
		return nil
	}

	edit := *a.editing
	a.editing = nil

	if edit.pane == "" {
		return nil
	}

	return screen.Batch{screen.UnstageEditor{}, a.deps.Clients.EditedWrite(edit.agent, nil)}
}

// ReconcileEditor catches the editor pane going away on its own and saves what it wrote.
func (a *Screen) ReconcileEditor(in screen.Inputs) screen.Request {
	if !a.IsEditorOpen() || a.deps.Panes.IsPaneAlive(a.editing.pane) {
		return nil
	}

	return screen.Batch{a.dropEdit(), screen.RefreshStage{}}
}

// LeaveScreen drops the rename being typed and takes down an editor open on the stage.
func (a *Screen) LeaveScreen() screen.Request {
	a.renaming = nil

	return a.dropEdit()
}
