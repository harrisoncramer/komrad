package review

import (
	"slices"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// worktreeEdit is the reader's editor over the agent's worktree, and the files it opened.
type worktreeEdit struct {
	// touched are the files the reader opened an editor on, oldest first, which is what the agent
	// is told about once the editor closes.
	touched []string

	// atEditor says your own editor holds the stage, so the panel renders its sidebar alone.
	atEditor    bool
	editingPath string
	// editorPane is the pane the editor was given, empty where the split was refused, which is
	// what says whether a pane going away is the editor being closed.
	editorPane string
}

// end puts the editor down and says which files the reader opened in it.
func (e *worktreeEdit) end() []string {
	touched := e.touched

	*e = worktreeEdit{}

	return touched
}

// openEditor records the editor holding the stage on one file, in the pane the split gave it.
func (e *worktreeEdit) openEditor(path, pane string) {
	e.atEditor = true
	e.editingPath = path
	e.editorPane = pane
	if !slices.Contains(e.touched, path) {
		e.touched = append(e.touched, path)
	}
}

// pressEdit is the edit key, which opens the editor and closes one already open.
func (r *Screen) pressEdit(in screen.Inputs) screen.Request {
	if r.isHistory() && !r.edit.atEditor {
		return nil
	}

	if r.edit.atEditor {
		return r.closeEdit(in)
	}

	if reason, refused := r.isEditRefused(in); refused {
		return screen.NewNote(screen.Warning, reason)
	}

	return r.editFile(in)
}

// isEditRefused returns why the edit key would open nothing.
func (r Screen) isEditRefused(in screen.Inputs) (string, bool) {
	if r.isWorktreeRemoved(in) {
		return worktreeRemovedSaid, true
	}

	if _, _, ok := r.diff.View().GetEditTarget(r.diff.Cursor()); !ok {
		return "that file was deleted, so there is nothing to open", true
	}

	if reason, unreadable := r.isAgentUnreadable(in); unreadable {
		return reason, true
	}

	if reason, refused := r.deps.EditorSettings.IsRemoteEditRefused(); refused {
		return reason, true
	}

	return "", false
}

// isAgentUnreadable reports whether komrad cannot say what the agent is doing.
func (r Screen) isAgentUnreadable(in screen.Inputs) (string, bool) {
	item, ok := r.agentRow(in)
	if !ok || !item.Alive || item.Agent.State != panel.StateUncertain {
		return "", false
	}

	return "komrad cannot read this agent's pane, so it may be writing in this worktree" + separated +
		"look at the pane before you edit", true
}

// editFile opens the file under the cursor in the agent's own worktree, in the stage beside the
// sidebar.
func (r *Screen) editFile(in screen.Inputs) screen.Request {
	path, line, ok := r.diff.View().GetEditTarget(r.diff.Cursor())
	if !ok {
		return screen.NewNote(screen.Warning, "that file was deleted, so there is nothing to open")
	}

	if reason, busy := r.isAgentBusy(in); busy {
		return screen.NewNote(screen.Warning, reason)
	}

	return screen.Stage{
		Content: r.deps.EditorSettings.FileContent(r.root, path, line),
		Staged:  func(pane string) tea.Msg { return editorStaged{path: path, pane: pane} },
	}
}

// editorStaged is the pane an editor over a file in the agent's worktree was given, empty
// where the split was refused.
type editorStaged struct {
	path string
	pane string
}

// applyEditorStaged keeps the editor's pane, or says there was no room for it.
func (r *Screen) applyEditorStaged(staged editorStaged) screen.Request {
	path, pane := staged.path, staged.pane
	if pane == "" {
		return screen.NewNote(screen.Warning, "there is no room beside the sidebar for an editor")
	}

	r.edit.openEditor(path, pane)

	return screen.Note{
		Kind:    screen.Info,
		Summary: "editing " + path,
		Detail:  keys.MenuChordLabel(r.deps.Keys, r.deps.Keys.Review.EditFile) + " to come back",
	}
}

// isAgentBusy reports whether the agent may be writing in the tree the editor would open.
func (r Screen) isAgentBusy(in screen.Inputs) (string, bool) {
	item, ok := r.agentRow(in)
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
		return r.isAgentUnreadable(in)
	case panel.StateWaiting, panel.StateIdle, panel.StateErrored, panel.StateSettingUp,
		panel.StateFailedToCreate, panel.StateFailedToCleanup:
	}

	return "", false
}

// agentRow hands back the listing's row for the agent whose diff is on screen.
func (r Screen) agentRow(in screen.Inputs) (panel.AgentRow, bool) {
	return in.Listing.LiveAgent(r.agentID)
}

// closeEdit gives the stage back to the diff, tells the agent which files the reader opened, and
// reads the worktree again with the reader's own writing in it.
func (r *Screen) closeEdit(in screen.Inputs) screen.Request {
	return screen.Batch{screen.Unstage{}, r.mailEdited(in), screen.Restage{}, r.catchUp()}
}

// mailEdited puts the editor down and tells the agent which files the reader opened in it.
func (r *Screen) mailEdited(in screen.Inputs) screen.Request {
	touched := r.edit.end()
	if len(touched) == 0 {
		return nil
	}

	item, ok := r.agentRow(in)
	if !ok {
		return nil
	}

	return r.deps.Clients.EditedWrite(item.Agent, touched)
}

// ReconcileEditor catches the editor pane going away on its own and takes the window back for the
// diff it was covering.
func (r *Screen) ReconcileEditor(in screen.Inputs) screen.Request {
	if !r.edit.atEditor {
		return nil
	}

	// A pane the split refused is not one that can go away, so nothing here reports it as the
	// editor closing.
	if r.edit.editorPane == "" || r.deps.Panes.IsPaneAlive(r.edit.editorPane) {
		return nil
	}

	return r.closeEdit(in)
}

// editorDesc says what the edit key would do.
func (e worktreeEdit) editorDesc() string {
	if e.atEditor {
		return "close the editor"
	}

	return "open in your editor"
}
