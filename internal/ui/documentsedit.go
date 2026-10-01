package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// documentEdit is one document open in your editor on the stage, from a copy of its text.
type documentEdit struct {
	item    documentItem
	version int32
	path    string
	pane    string
	body    string
}

// pressEdit opens the document under the cursor in your editor, or saves and closes the one open.
func (d *documentsScreen) pressEdit(m *model) tea.Cmd {
	if d.editing != nil {
		return d.closeEdit(m)
	}

	if m.isScratchOpen() {
		m.reportBrief("could not open the editor", "close the scratch file first")

		return nil
	}

	item, ok := d.hovered(m)
	if !ok {
		return nil
	}

	read, asked := d.bodies[item.id]
	if !asked || read.err != nil || read.body == "" || read.pending || read.updatedAt.Before(item.updatedAt) {
		m.reportBrief("could not edit "+item.name, "its text has not been read yet")

		return nil
	}

	path, err := writeDocumentCopy(m.settings.DraftDir, read.body)
	if err != nil {
		m.report("could not edit "+item.name, err)

		return nil
	}

	pane := m.stageEditor(editorContent(m.settings.Editor, filepath.Dir(path), path, 1))
	if pane == "" {
		m.report("could not edit "+item.name, removeDocumentCopy(path))
		m.reportBrief("could not edit "+item.name, "there is no room beside the sidebar for an editor")

		return nil
	}

	d.history = nil
	d.editing = &documentEdit{
		item:    item,
		version: read.version,
		path:    path,
		pane:    pane,
		body:    read.body,
	}

	return nil
}

// closeEdit takes the editor down and writes back what changed in the copy.
func (d *documentsScreen) closeEdit(m *model) tea.Cmd {
	if d.editing == nil {
		return nil
	}

	edit := *d.editing
	d.editing = nil
	m.unstageEditor()

	summary := "could not save " + edit.item.name
	content, err := os.ReadFile(edit.path)
	if err != nil {
		m.report(summary, fmt.Errorf("failed to read your copy at %s: %w", edit.path, err))

		return nil
	}

	body := string(content)
	if strings.TrimSpace(body) == "" {
		m.reportBrief(summary, "an empty document is not saved, so your copy is left at "+edit.path)

		return nil
	}

	if body == edit.body {
		m.report(summary, removeDocumentCopy(edit.path))

		return nil
	}

	return m.writeCmd("saving "+edit.item.name, summary, func(a api) error {
		if err := a.writeDocument(edit.item, edit.version, body); err != nil {
			return fmt.Errorf("failed to save, so your copy is left at %s: %w", edit.path, err)
		}

		return removeDocumentCopy(edit.path)
	})
}

// reconcileEditor catches the editor pane going away on its own and saves what it wrote.
func (d *documentsScreen) reconcileEditor(m *model) tea.Cmd {
	if d.editing == nil || m.isPaneAlive(d.editing.pane) {
		return nil
	}

	return d.closeEdit(m)
}

// editLabel says what the edit entry would do.
func (d *documentsScreen) editLabel() string {
	if d.editing != nil {
		return "close editor"
	}

	return "edit"
}

func writeDocumentCopy(dir, body string) (string, error) {
	file, err := os.CreateTemp(dir, "komrad-document-*.md")
	if err != nil {
		return "", fmt.Errorf("failed to create a copy to edit: %w", err)
	}

	_, writeErr := file.WriteString(body)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return "", fmt.Errorf("failed to write a copy to edit at %s: %w", file.Name(), errors.Join(writeErr, closeErr))
	}

	return file.Name(), nil
}

func removeDocumentCopy(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove your copy at %s: %w", path, err)
	}

	return nil
}
