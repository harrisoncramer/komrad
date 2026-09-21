package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	agentstore "weave/internal/store/agent"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
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

// pressReviewEdit is the edit key, refused on a round the agent has written past, and the way
// into a pass when the reader is still only watching.
func (m model) pressReviewEdit() (tea.Model, tea.Cmd) {
	if said, history := m.isReviewHistory(); history && !m.review.atEditor {
		m.review.notice = said + " is finished, so the worktree no longer holds these lines · " +
			keyLabel(m.keys.Review.BaseForward) + " for the current work"

		return m, nil
	}

	// A press that arrives while the agent is still coming to a stop is the wait, not a second
	// thing to do, and letting it through would open the editor the wait exists to hold shut.
	if m.review.pausing {
		m.review.notice = "the agent is still stopping"

		return m, nil
	}

	if m.review.mode != modeReviewing {
		// The refusals are read before the agent is stopped, since stopping one to then say the
		// key was never going to open anything is a real thing done for nothing.
		if reason, refused := m.isReviewEditRefused(); refused {
			m.review.notice = reason

			return m, nil
		}

		return m.startReviewPass(intentEdit)
	}

	return m.editReviewFile()
}

// isReviewEditRefused is why the edit key would open nothing, read before a pass is opened for it.
func (m model) isReviewEditRefused() (string, bool) {
	if _, _, ok := m.review.view.GetEditTarget(m.review.cursor); !ok {
		return "that file was deleted, so there is nothing to open", true
	}

	if reason, unreadable := m.isReviewAgentUnreadable(); unreadable {
		return reason, true
	}

	if reason, owed := m.isReviewOwedAnAnswer(); owed {
		return reason, true
	}

	return "", false
}

// isReviewAgentUnreadable reports whether weave cannot say what the agent is doing, which is the
// one state a pass may not stop its way out of.
func (m model) isReviewAgentUnreadable() (string, bool) {
	item, ok := m.getReviewAgentItem()
	if !ok || !item.alive || item.agent.State != agentstore.StateUncertain {
		return "", false
	}

	return "weave cannot read this agent's pane, so it may be writing in this worktree · " +
		"look at the pane before you edit", true
}

// editReviewFile opens the file under the cursor in the agent's own worktree, in the stage beside
// the sidebar.
func (m model) editReviewFile() (tea.Model, tea.Cmd) {
	if m.review.atEditor {
		return m.closeReviewEdit()
	}

	path, line, ok := m.review.view.GetEditTarget(m.review.cursor)
	if !ok {
		m.review.notice = "that file was deleted, so there is nothing to open"

		return m, nil
	}

	if reason, busy := m.isReviewAgentBusy(); busy {
		m.review.notice = reason

		return m, nil
	}

	if reason, owed := m.isReviewOwedAnAnswer(); owed {
		m.review.notice = reason

		return m, nil
	}

	m.frame.Show(layout.RegionStage, editorContent(m.editor, m.review.root, path, line))

	// The mode is taken only once there is a pane in it, a refused split otherwise leaving the
	// band saying editing over a screen with no editor on it.
	pane := m.frame.Pane(layout.RegionStage)
	if pane == "" {
		m.review.notice = "there is no room beside the sidebar for an editor"

		return m, nil
	}

	m.review.atEditor = true
	m.review.editingPath = path
	m.review.editorPane = pane
	if !slices.Contains(m.review.touched, path) {
		m.review.touched = append(m.review.touched, path)
	}
	m.tmux.SelectPane(pane)

	return m, nil
}

// isReviewOwedAnAnswer reports whether comments stand against this work, and what to say about it.
func (m model) isReviewOwedAnAnswer() (string, bool) {
	if !m.review.isOwedAnAnswer() {
		return "", false
	}

	if _, unreachable := m.isReviewReadOnly(); unreachable {
		return "", false
	}

	return reviewOwedSaid(m.reviewOwedComments()) + " · " +
		keyLabel(m.keys.Review.Resolve) + " settles one, " +
		keyLabel(m.keys.Review.NextOpenThread) + " walks them", true
}

// isReviewAgentBusy reports whether the agent may be writing in the tree the editor would open.
func (m model) isReviewAgentBusy() (string, bool) {
	// A pass stops the agent before it opens anything, and an agent that has been seen to stop is
	// not writing, whatever the state on its row was read as before that.
	if m.review.paused {
		return "", false
	}

	item, ok := m.getReviewAgentItem()
	if !ok || !item.alive {
		return "", false
	}

	// No default, so a state added later fails the exhaustive check here rather than quietly
	// letting an editor in over a writer.
	switch item.agent.State {
	case agentstore.StateThinking:
		return "the agent is writing in this worktree · " +
			"two writers in one checkout cannot both win, so stop it before you edit", true
	case agentstore.StateUncertain:
		return m.isReviewAgentUnreadable()
	case agentstore.StateWaiting, agentstore.StateIdle, agentstore.StateSettingUp,
		agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
	}

	return "", false
}

// getReviewAgentItem hands back the listing's row for the agent whose diff is on screen.
func (m model) getReviewAgentItem() (agentItem, bool) {
	for _, item := range m.listing.agents {
		if item.agent.ID == m.review.agentID {
			return item, true
		}
	}

	return agentItem{}, false
}

// closeReviewEdit gives the stage back to the diff and reads the worktree again, staying in the
// pass, since closing an editor is not the end of reading somebody's work.
func (m model) closeReviewEdit() (tea.Model, tea.Cmd) {
	m.review.atEditor = false
	m.review.editingPath = ""
	m.review.editorPane = ""

	m.frame.Show(layout.RegionStage, newBlankContent())
	m.tmux.SelectPane(m.frame.Pane(layout.RegionSidebar))

	return m.reloadReview()
}

// reconcileEditor catches the editor pane going away on its own and takes the window back for the
// diff it was covering.
func (m model) reconcileEditor() (model, tea.Cmd) {
	m.reconcileInfo()

	if !m.review.atEditor || m.screen != screenReview {
		return m, nil
	}

	// A pane the split refused is not one that can go away, so nothing here reports it as the
	// editor closing.
	if m.review.editorPane == "" || m.tmux.IsPaneAlive(m.review.editorPane) {
		return m, nil
	}

	next, cmd := m.closeReviewEdit()

	written, ok := next.(model)
	if !ok {
		return m, cmd
	}

	return written, cmd
}

func (m model) reviewEditorDesc() string {
	if m.review.atEditor {
		return "close the editor"
	}

	return "open in your editor"
}
