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

// editorContent opens a file in the user's editor, on one line of it. The editor is left
// unquoted so an EDITOR of several words is the words it is made of, and the file is opened
// from the repository root rather than beside itself, since that is the project the editor is
// being pointed at.
func editorContent(editor, dir, path string, line int) layout.Content {
	command := strings.TrimSpace(editor)
	if command == "" {
		command = defaultEditor
	}

	open := fmt.Sprintf("cd %s && exec %s +%d %s", shellQuote(dir), command, line, shellQuote(path))

	return layout.Content{
		Key:    fmt.Sprintf("edit:%s:%d", path, line),
		Render: func(layout.Target) ([]string, bool) { return []string{"sh", "-c", open}, true },
	}
}

// editReviewFile opens the file under the cursor in a split under the diff, on the line the
// cursor is standing on, and takes that split away again when it is already open there.
func (m model) editReviewFile() (tea.Model, tea.Cmd) {
	path, line, ok := m.review.view.EditTarget(m.review.cursor)
	if !ok {
		m.review.notice = "that file was deleted, so there is nothing to open"

		return m, nil
	}

	target := fmt.Sprintf("%s:%d", path, line)
	if m.info.kind == infoEditFile && m.info.action == target {
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

// remeasureReview draws the diff again at the size the stage is left with. The stage gives rows
// up to a split under it and takes them back when it goes, and the drawer is handed exactly one
// screenful, so a stale measurement is rows the pane cannot show.
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
