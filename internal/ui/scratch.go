package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

var errNoScratchDir = errors.New("the panel was started without a directory for scratch files")

func (m *model) isScratchOpen() bool {
	return m.scratchPane != ""
}

// toggleScratch puts the engine's scratch file on the stage in the user's editor, or takes it
// down when it is already up.
func (m *model) toggleScratch() tea.Cmd {
	if m.isScratchOpen() {
		return m.closeScratch()
	}

	if m.screen == screenReview && m.review.hold.atEditor {
		m.reportBrief("could not open the scratch file", "close the review's editor first")

		return nil
	}

	path, err := m.ensureScratch()
	if err != nil {
		m.report("Could not open the scratch file", err)

		return nil
	}

	left := m.leaveScreen()

	pane := m.stageEditor(editorContent(m.settings.Editor, filepath.Dir(path), path, 1))
	if pane == "" {
		m.reportBrief("could not open the scratch file", "there is no room beside the sidebar for an editor")

		return tea.Batch(left, m.waitToStage())
	}

	m.scratchPane = pane
	m.reconcileSolo()

	return left
}

func (m *model) closeScratch() tea.Cmd {
	m.scratchPane = ""
	m.unstageEditor()
	m.reconcileSolo()

	return m.waitToStage()
}

// reconcileScratch catches the scratch editor quitting on its own.
func (m *model) reconcileScratch() tea.Cmd {
	if !m.isScratchOpen() || m.tmux.IsPaneAlive(m.scratchPane) {
		return nil
	}

	return m.closeScratch()
}

// ensureScratch is the engine's scratch file, created empty the first time it is opened.
func (m *model) ensureScratch() (string, error) {
	dir := m.settings.ScratchDir
	if dir == "" {
		return "", errNoScratchDir
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", dir, err)
	}

	name := strings.ReplaceAll(engineTitle(m.engineStatus.Name), string(filepath.Separator), "-")
	path := filepath.Join(dir, name+".md")

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("failed to create %s: %w", path, err)
	}

	if err := file.Close(); err != nil {
		return "", fmt.Errorf("failed to close %s: %w", path, err)
	}

	return path, nil
}
