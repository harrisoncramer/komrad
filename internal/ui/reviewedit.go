package ui

import (
	"slices"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// reviewEdit is the reader's editor over the agent's worktree, and the files it opened.
type reviewEdit struct {
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
func (p *reviewEdit) end() []string {
	touched := p.touched

	*p = reviewEdit{}

	return touched
}

// openEditor records the editor holding the stage on one file, in the pane the split gave it.
func (p *reviewEdit) openEditor(path, pane string) {
	p.atEditor = true
	p.editingPath = path
	p.editorPane = pane
	if !slices.Contains(p.touched, path) {
		p.touched = append(p.touched, path)
	}
}

// pressEdit is the edit key, which opens the editor and closes one already open.
func (r *reviewScreen) pressEdit(m *model) screen.Request {
	if r.isHistory() && !r.edit.atEditor {
		return nil
	}

	if r.edit.atEditor {
		return r.closeEdit(m)
	}

	if reason, refused := r.isEditRefused(m); refused {
		return screen.NewNote(screen.Warning, reason)
	}

	return r.editFile(m)
}

// isEditRefused returns why the edit key would open nothing.
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
// sidebar.
func (r *reviewScreen) editFile(m *model) screen.Request {
	path, line, ok := r.diff.View().GetEditTarget(r.diff.Cursor())
	if !ok {
		return screen.NewNote(screen.Warning, "that file was deleted, so there is nothing to open")
	}

	if reason, busy := r.isAgentBusy(m); busy {
		return screen.NewNote(screen.Warning, reason)
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

// applyEditorStaged keeps the editor's pane, or says there was no room for it.
func (r *reviewScreen) applyEditorStaged(m *model, staged reviewEditorStaged) screen.Request {
	path, pane := staged.path, staged.pane
	if pane == "" {
		return screen.NewNote(screen.Warning, "there is no room beside the sidebar for an editor")
	}

	r.edit.openEditor(path, pane)

	return screen.Note{
		Kind:    screen.Info,
		Summary: "editing " + path,
		Detail:  m.menuChordLabel(r.deps.keys.Review.EditFile) + " to come back",
	}
}

// isAgentBusy reports whether the agent may be writing in the tree the editor would open.
func (r reviewScreen) isAgentBusy(m *model) (string, bool) {
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

// closeEdit gives the stage back to the diff, tells the agent which files the reader opened, and
// reads the worktree again with the reader's own writing in it.
func (r *reviewScreen) closeEdit(m *model) screen.Request {
	return screen.Batch{screen.Unstage{}, r.endEdit(m), screen.Restage{}, r.catchUp()}
}

// endEdit puts the editor down and tells the agent which files the reader opened in it.
func (r *reviewScreen) endEdit(m *model) screen.Request {
	touched := r.edit.end()
	if len(touched) == 0 {
		return nil
	}

	item, ok := r.agentRow(m)
	if !ok {
		return nil
	}

	return r.deps.clients.EditedWrite(item.Agent, touched)
}

// reconcileEditor catches the editor pane going away on its own and takes the window back for the
// diff it was covering.
func (r *reviewScreen) reconcileEditor(m *model, in screen.Inputs) screen.Request {
	if !r.edit.atEditor {
		return nil
	}

	// A pane the split refused is not one that can go away, so nothing here reports it as the
	// editor closing.
	if r.edit.editorPane == "" || r.deps.panes.IsPaneAlive(r.edit.editorPane) {
		return nil
	}

	return r.closeEdit(m)
}

// editorDesc says what the edit key would do.
func (p reviewEdit) editorDesc() string {
	if p.atEditor {
		return "close the editor"
	}

	return "open in your editor"
}
