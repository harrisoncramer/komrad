package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/ui/screen"

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
func (d *documentsScreen) pressEdit(m *model) screen.Request {
	if d.editing != nil {
		return d.closeEdit(m)
	}

	item, ok := d.hovered(m)
	if !ok {
		return nil
	}

	read, asked := d.bodies[item.id]
	if !asked || read.err != nil || read.body == "" || read.pending || read.updatedAt.Before(item.updatedAt) {
		return screen.Note{Kind: screen.Error, Summary: "could not edit " + item.name, Detail: "its text has not been read yet"}
	}

	path, err := writeDocumentCopy(m.settings.DraftDir, read.body)
	if err != nil {
		return screen.Report{Summary: "could not edit " + item.name, Err: err}
	}

	return screen.Stage{
		Content: editorContent(m.settings.Editor, filepath.Dir(path), path, 1),
		Staged: func(pane string) tea.Msg {
			return documentEditorStaged{item: item, read: read, path: path, pane: pane}
		},
	}
}

// documentEditorStaged is the pane an editor over a document's copy was given, empty where the
// split was refused.
type documentEditorStaged struct {
	item documentItem
	read documentBody
	path string
	pane string
}

// applyEditorStaged opens the edit in the editor's pane, or throws the copy away when there was no
// room for one.
func (d *documentsScreen) applyEditorStaged(staged documentEditorStaged) screen.Request {
	item, read, path, pane := staged.item, staged.read, staged.path, staged.pane
	if pane == "" {
		return screen.Batch{
			screen.Report{Summary: "could not edit " + item.name, Err: removeDocumentCopy(path)},
			screen.Note{Kind: screen.Error, Summary: "could not edit " + item.name, Detail: "there is no room beside the sidebar for an editor"},
		}
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
func (d *documentsScreen) closeEdit(m *model) screen.Request {
	if d.editing == nil {
		return nil
	}

	edit := *d.editing
	d.editing = nil
	m.unstageEditor()

	summary := "could not save " + edit.item.name
	content, err := os.ReadFile(edit.path)
	if err != nil {
		return screen.Report{Summary: summary, Err: fmt.Errorf("failed to read your copy at %s: %w", edit.path, err)}
	}

	body := string(content)
	if strings.TrimSpace(body) == "" {
		return screen.Note{Kind: screen.Error, Summary: summary, Detail: "komrad does not save an empty document, so your copy stays at " + edit.path}
	}

	if body == edit.body {
		return screen.Report{Summary: summary, Err: removeDocumentCopy(edit.path)}
	}

	return m.write("saving "+edit.item.name, summary, func(a api) error {
		if err := a.writeDocument(edit.item, edit.version, body); err != nil {
			return fmt.Errorf("failed to save, so your copy is left at %s: %w", edit.path, err)
		}

		return removeDocumentCopy(edit.path)
	})
}

// reconcileEditor catches the editor pane going away on its own and saves what it wrote.
func (d *documentsScreen) reconcileEditor(m *model) screen.Request {
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
