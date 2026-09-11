package ui

import (
	"fmt"
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
	return fmt.Sprintf("exec %s +%d %s", command, line, shellQuote(path))
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

// pressReviewEdit is the edit key, which is refused on a round the agent has written past since
// the file on disk is no longer the file those line numbers name.
func (m model) pressReviewEdit() (tea.Model, tea.Cmd) {
	if said, history := m.reviewIsHistory(); history && !m.review.atEditor {
		m.review.notice = said + " is finished, so the worktree no longer holds these lines · " +
			keyLabel(m.keys.Review.BaseForward) + " for the current work"

		return m, nil
	}

	next, cmd, _ := m.editReviewFile()

	return next, cmd
}

// editReviewFile opens the file under the cursor in the agent's own worktree, in the stage beside
// the sidebar, and reports whether anything was opened.
func (m model) editReviewFile() (tea.Model, tea.Cmd, bool) {
	if m.review.atEditor {
		next, cmd := m.closeReviewEdit()

		return next, cmd, false
	}

	path, line, ok := m.review.view.GetEditTarget(m.review.cursor)
	if !ok {
		m.review.notice = "that file was deleted, so there is nothing to open"

		return m, nil, false
	}

	if reason, busy := m.reviewIsAgentBusy(); busy {
		m.review.notice = reason

		return m, nil, false
	}

	m.frame.Show(layout.RegionStage, editorContent(m.editor, m.review.dir, path, line))

	// The mode is taken only once there is a pane in it, a refused split otherwise leaving the
	// band saying editing over a screen with no editor on it.
	pane := m.frame.Pane(layout.RegionStage)
	if pane == "" {
		m.review.notice = "there is no room beside the sidebar for an editor"

		return m, nil, false
	}

	m.review.mode = modeEditing
	m.review.atEditor = true
	m.review.editingPath = path
	m.review.editorPane = pane
	m.tmux.SelectPane(pane)

	return m, nil, true
}

// reviewIsAgentBusy is whether anything says the agent may be writing in the tree the editor
// would open, and what to say about it.
func (m model) reviewIsAgentBusy() (string, bool) {
	item, ok := m.reviewAgentItem()
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
		// Explainer: uncertain is weave failing to read the pane rather than the agent doing
		// nothing, so an agent part way through a build or a screen weave has no pattern for sits
		// here while it writes. Nothing the panel knows can say the tree is idle, and the editor is
		// the one place that has to be sure.
		return "weave cannot read this agent's pane, so it may be writing in this worktree · " +
			"look at the pane before you edit", true
	case agentstore.StateWaiting, agentstore.StateIdle, agentstore.StateSettingUp,
		agentstore.StateFailedToCreate, agentstore.StateFailedToCleanup:
	}

	return "", false
}

// reviewAgentItem is the listing's row for the agent whose diff is on screen.
func (m model) reviewAgentItem() (agentItem, bool) {
	for _, item := range m.listing.agents {
		if item.agent.ID == m.review.agentID {
			return item, true
		}
	}

	return agentItem{}, false
}

// closeReviewEdit gives the stage back to the diff and reads the worktree again, since what the
// editor wrote is part of the change from now on.
func (m model) closeReviewEdit() (tea.Model, tea.Cmd) {
	m.review.mode = modeWatching
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
