package ui

import (
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// pressEdit is the edit key, which stops the agent before it opens the editor and closes one
// already open.
func (r *reviewScreen) pressEdit(m *model) screen.Request {
	if r.isHistory() && !r.hold.atEditor {
		return nil
	}

	// A press that arrives while the agent is still coming to a stop is the wait, not a second
	// thing to do, and letting it through would open the editor the wait exists to hold shut.
	if r.hold.pausing {
		return screen.NewNote(screen.Warning, "the agent is still stopping")
	}

	if r.hold.atEditor {
		return r.closeEdit(m)
	}

	// The refusals are read before the agent is stopped.
	if reason, refused := r.isEditRefused(m); refused {
		return screen.NewNote(screen.Warning, reason)
	}

	return r.startHold(m)
}

// isEditRefused returns why the edit key would open nothing, before the panel stops the agent.
func (r reviewScreen) isEditRefused(m *model) (string, bool) {
	if r.isWorktreeRemoved(m) {
		return worktreeRemovedSaid, true
	}

	if _, _, ok := r.diff.View().GetEditTarget(r.diff.Cursor()); !ok {
		return "that file was deleted, so there is nothing to open", true
	}

	if reason, unreadable := r.isAgentUnreadable(m); unreadable {
		return reason, true
	}

	if reason, refused := r.deps.editorSettings.IsRemoteEditRefused(); refused {
		return reason, true
	}

	return "", false
}

// isAgentUnreadable reports whether komrad cannot say what the agent is doing.
func (r reviewScreen) isAgentUnreadable(m *model) (string, bool) {
	item, ok := r.agentRow(m)
	if !ok || !item.Alive || item.Agent.State != panel.StateUncertain {
		return "", false
	}

	return "komrad cannot read this agent's pane, so it may be writing in this worktree" + separated +
		"look at the pane before you edit", true
}

// editFile opens the file under the cursor in the agent's own worktree, in the stage beside the
// sidebar, letting the agent go again when there is nothing to open it in.
func (r *reviewScreen) editFile(m *model) screen.Request {
	path, line, ok := r.diff.View().GetEditTarget(r.diff.Cursor())
	if !ok {
		return screen.Batch{screen.NewNote(screen.Warning, "that file was deleted, so there is nothing to open"), r.endHold(m, abandonedSaid)}
	}

	if reason, busy := r.isAgentBusy(m); busy {
		return screen.Batch{screen.NewNote(screen.Warning, reason), r.endHold(m, abandonedSaid)}
	}

	return screen.Stage{
		Content: r.deps.editorSettings.FileContent(r.root, path, line),
		Staged:  func(pane string) tea.Msg { return reviewEditorStaged{path: path, pane: pane} },
	}
}

// reviewEditorStaged is the pane an editor over a file in the agent's worktree was given, empty
// where the split was refused.
type reviewEditorStaged struct {
	path string
	pane string
}

// applyEditorStaged keeps the editor's pane, or lets the agent go again when there was no room
// for it.
func (r *reviewScreen) applyEditorStaged(m *model, staged reviewEditorStaged) screen.Request {
	path, pane := staged.path, staged.pane
	if pane == "" {
		return screen.Batch{screen.NewNote(screen.Warning, "there is no room beside the sidebar for an editor"), r.endHold(m, abandonedSaid)}
	}

	r.hold.openEditor(path, pane)

	return screen.Note{
		Kind:    screen.Info,
		Summary: "editing " + path,
		Detail:  m.menuChordLabel(r.deps.keys.Review.EditFile) + " to come back",
	}
}

// isAgentBusy reports whether the agent may be writing in the tree the editor would open.
func (r reviewScreen) isAgentBusy(m *model) (string, bool) {
	// The panel stops the agent before the editor opens, and an agent seen to stop is not writing,
	// whatever its row read as before that.
	if r.hold.paused {
		return "", false
	}

	item, ok := r.agentRow(m)
	if !ok || !item.Alive {
		return "", false
	}

	// No default, so a state added later fails the exhaustive check here rather than quietly
	// letting an editor in over a writer.
	switch item.Agent.State {
	case panel.StateThinking:
		return "the agent is writing in this worktree" + separated +
			"two writers in one checkout cannot both win, so stop it before you edit", true
	case panel.StateUncertain:
		return r.isAgentUnreadable(m)
	case panel.StateWaiting, panel.StateIdle, panel.StateErrored, panel.StateSettingUp,
		panel.StateFailedToCreate, panel.StateFailedToCleanup:
	}

	return "", false
}

// agentRow hands back the listing's row for the agent whose diff is on screen.
func (r reviewScreen) agentRow(m *model) (panel.AgentRow, bool) {
	return m.listing.LiveAgent(r.agentID)
}

// closeEdit gives the stage back to the diff, lets the agent go, and reads the worktree again with
// the reader's own writing in it.
func (r *reviewScreen) closeEdit(m *model) screen.Request {
	return screen.Batch{screen.Unstage{}, r.endHold(m, editedSaid), screen.Restage{}, r.catchUp()}
}

// reconcileEditor catches the editor pane going away on its own and takes the window back for the
// diff it was covering.
func (r *reviewScreen) reconcileEditor(m *model, in screen.Inputs) screen.Request {
	if !r.hold.atEditor {
		return nil
	}

	// A pane the split refused is not one that can go away, so nothing here reports it as the
	// editor closing.
	if r.hold.editorPane == "" || r.deps.panes.IsPaneAlive(r.hold.editorPane) {
		return nil
	}

	return r.closeEdit(m)
}

// editorDesc says what the edit key would do.
func (p reviewHold) editorDesc() string {
	if p.atEditor {
		return "close the editor"
	}

	return "open in your editor"
}
