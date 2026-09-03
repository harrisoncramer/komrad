package ui

import (
	"fmt"
	"strings"

	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
)

// defaultEditor is what the file is opened in when nothing said otherwise, which is what the
// environment falls back to as well.
const defaultEditor = "vi"

// editorContent opens a file in the user's editor, on one line of it.
func editorContent(editor, dir, path string, line int) layout.Content {
	command := strings.TrimSpace(editor)
	if command == "" {
		command = defaultEditor
	}

	// The editor is left unquoted so an EDITOR of several words stays several words.
	open := fmt.Sprintf("exec %s +%d %s", command, line, shellQuote(path))

	return layout.Content{
		Key:    fmt.Sprintf("edit:%s:%d", path, line),
		Dir:    dir,
		Render: func(layout.Target) ([]string, bool) { return []string{"sh", "-c", open}, true },
	}
}

// editReviewFile opens the file under the cursor in a split under the diff, on the line the
// cursor is standing on, and takes that split away again when it is already open there.
func (m model) editReviewFile() (tea.Model, tea.Cmd) {
	path, line, ok := m.review.view.GetEditTarget(m.review.cursor)
	if !ok {
		m.review.notice = "that file was deleted, so there is nothing to open"

		return m, nil
	}

	target := fmt.Sprintf("%s:%d", path, line)
	if m.info.kind == infoEditFile && m.info.subject == target {
		return m.closeReviewEdit()
	}

	if !m.claimInfo(infoEditFile, target) {
		return m, nil
	}

	m.frame.Show(layout.RegionInfo, editorContent(m.editor, m.review.root, path, line))
	m.remeasureReview()
	m.tmux.SelectPane(m.frame.Pane(layout.RegionInfo))

	return m, nil
}

func (m model) closeReviewEdit() (tea.Model, tea.Cmd) {
	m = m.closeInfo()
	m.remeasureReview()

	return m.reloadReview()
}

// remeasureReview draws the diff again at the size the stage is left with, since the drawer is
// handed exactly one screenful and a stale size is rows the pane cannot show.
func (m *model) remeasureReview() {
	m.frame.Claimed(layout.RegionStage)
	m.stageReview()
}

func (m model) reviewEditorDesc() string {
	if m.info.kind == infoEditFile {
		return "close the editor"
	}

	return "open in your editor"
}
