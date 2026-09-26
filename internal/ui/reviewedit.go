package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"komrad/internal/ui/layout"
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// defaultEditor is what the file is opened in when nothing said otherwise.
const defaultEditor = "vi"

// editorShell is the shell line that runs the user's editor on a file, on one line of it.
func editorShell(editor, path string, line int) string {
	command := strings.TrimSpace(editor)
	if command == "" {
		command = defaultEditor
	}

	// The editor is left unquoted so an EDITOR of several words stays several words.
	return "exec " + command + " " + strings.Join(editorLineArgs(command, path, line), " ")
}

// editorLineArgs is how one editor is told which line to open a file on, defaulting to the +line
// argument every terminal editor in the vi and emacs line takes.
func editorLineArgs(command, path string, line int) []string {
	at := shellQuote(fmt.Sprintf("%s:%d", path, line))

	switch editorName(command) {
	case "code", "code-insiders", "codium", "vscodium", "cursor", "windsurf":
		return []string{"--wait", "--goto", at}
	case "subl", "sublime_text", "zed":
		return []string{"--wait", at}
	case "hx", "helix":
		return []string{at}
	case "goland", "idea", "pycharm", "webstorm", "phpstorm", "rubymine", "clion", "rider",
		"rustrover", "datagrip":
		return []string{"--wait", "--line", fmt.Sprint(line), shellQuote(path)}
	}

	return []string{fmt.Sprintf("+%d", line), shellQuote(path)}
}

// editorName is the program an EDITOR of several words runs.
func editorName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}

	return filepath.Base(fields[0])
}

// editorContent opens a file in the user's editor, on one line of it.
func editorContent(editor, dir, path string, line int) layout.Content {
	open := editorShell(editor, path, line)

	return layout.Content{
		Key:    fmt.Sprintf("edit:%s:%d", path, line),
		Dir:    dir,
		Render: func(layout.Target) ([]string, bool) { return []string{"sh", "-c", open}, true },
	}
}

// pressEdit is the edit key, refused on a round the agent has written past, and the way into a
// pass when the reader is still only watching.
func (r *reviewScreen) pressEdit(h host, s panelShared) tea.Cmd {
	if said, history := r.isHistory(); history && !r.pass.atEditor {
		r.notice = said + " is finished, so the worktree no longer holds these lines · " +
			keyLabel(s.keys.Review.BaseForward) + " for the current work"

		return nil
	}

	// A press that arrives while the agent is still coming to a stop is the wait, not a second
	// thing to do, and letting it through would open the editor the wait exists to hold shut.
	if r.pass.pausing {
		r.notice = "the agent is still stopping"

		return nil
	}

	if r.pass.mode != modeReviewing {
		// The refusals are read before the agent is stopped.
		if reason, refused := r.isEditRefused(s); refused {
			r.notice = reason

			return nil
		}

		return r.startPass(h, s, intentEdit)
	}

	return r.editFile(h, s)
}

// isEditRefused is why the edit key would open nothing, read before a pass is opened for it.
func (r reviewScreen) isEditRefused(s panelShared) (string, bool) {
	if _, _, ok := r.diff.view.GetEditTarget(r.diff.cursor); !ok {
		return "that file was deleted, so there is nothing to open", true
	}

	if reason, unreadable := r.isAgentUnreadable(s); unreadable {
		return reason, true
	}

	if reason, owed := r.isOwedAnAnswer(s); owed {
		return reason, true
	}

	return "", false
}

// isAgentUnreadable reports whether komrad cannot say what the agent is doing.
func (r reviewScreen) isAgentUnreadable(s panelShared) (string, bool) {
	item, ok := r.agentRow(s)
	if !ok || !item.Alive || item.Agent.State != panel.StateUncertain {
		return "", false
	}

	return "komrad cannot read this agent's pane, so it may be writing in this worktree · " +
		"look at the pane before you edit", true
}

// editFile opens the file under the cursor in the agent's own worktree, in the stage beside the
// sidebar.
func (r *reviewScreen) editFile(h host, s panelShared) tea.Cmd {
	if r.pass.atEditor {
		return r.closeEdit(h, s)
	}

	path, line, ok := r.diff.view.GetEditTarget(r.diff.cursor)
	if !ok {
		r.notice = "that file was deleted, so there is nothing to open"

		return nil
	}

	if reason, busy := r.isAgentBusy(s); busy {
		r.notice = reason

		return nil
	}

	if reason, owed := r.isOwedAnAnswer(s); owed {
		r.notice = reason

		return nil
	}

	// The mode is taken only once there is a pane in it, a refused split otherwise leaving the
	// band saying editing over a screen with no editor on it.
	pane := h.stageEditor(editorContent(s.editor, r.root, path, line))
	if pane == "" {
		r.notice = "there is no room beside the sidebar for an editor"

		return nil
	}

	r.pass.openEditor(path, pane)

	return nil
}

// isOwedAnAnswer reports whether comments stand against this work, and what to say about it.
func (r reviewScreen) isOwedAnAnswer(s panelShared) (string, bool) {
	if !r.threads.isOwedAnAnswer() {
		return "", false
	}

	if _, unreachable := r.isReadOnly(s); unreachable {
		return "", false
	}

	return reviewOwedSaid(r.owedComments()) + " · " +
		keyLabel(s.keys.Review.Resolve) + " settles one, " +
		keyLabel(s.keys.Review.NextOpenThread) + " walks them", true
}

// isAgentBusy reports whether the agent may be writing in the tree the editor would open.
func (r reviewScreen) isAgentBusy(s panelShared) (string, bool) {
	// A pass stops the agent before it opens anything, and an agent that has been seen to stop is
	// not writing, whatever the state on its row was read as before that.
	if r.pass.paused {
		return "", false
	}

	item, ok := r.agentRow(s)
	if !ok || !item.Alive {
		return "", false
	}

	// No default, so a state added later fails the exhaustive check here rather than quietly
	// letting an editor in over a writer.
	switch item.Agent.State {
	case panel.StateThinking:
		return "the agent is writing in this worktree · " +
			"two writers in one checkout cannot both win, so stop it before you edit", true
	case panel.StateUncertain:
		return r.isAgentUnreadable(s)
	case panel.StateWaiting, panel.StateIdle, panel.StateSettingUp,
		panel.StateFailedToCreate, panel.StateFailedToCleanup:
	}

	return "", false
}

// agentRow hands back the listing's row for the agent whose diff is on screen.
func (r reviewScreen) agentRow(s panelShared) (panel.AgentRow, bool) {
	return s.listing.liveAgent(r.agentID)
}

// closeEdit gives the stage back to the diff and reads the worktree again, staying in the pass.
func (r *reviewScreen) closeEdit(h host, s panelShared) tea.Cmd {
	r.pass.closeEditor()
	h.unstageEditor()

	return r.reload(s)
}

// reconcileEditor catches the editor pane going away on its own and takes the window back for the
// diff it was covering.
func (r *reviewScreen) reconcileEditor(h host, s panelShared) tea.Cmd {
	if !r.pass.atEditor {
		return nil
	}

	// A pane the split refused is not one that can go away, so nothing here reports it as the
	// editor closing.
	if r.pass.editorPane == "" || h.isPaneAlive(r.pass.editorPane) {
		return nil
	}

	return r.closeEdit(h, s)
}

// editorDesc says what the edit key would do.
func (p reviewPass) editorDesc() string {
	if p.atEditor {
		return "close the editor"
	}

	return "open in your editor"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
