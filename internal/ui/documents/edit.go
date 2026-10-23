package documents

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/ui/api"
	"komrad/internal/ui/editor"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// editing is one document open in your editor on the stage, from a copy of its text.
type editing struct {
	item    document
	version int32
	path    string
	pane    string
	body    string
}

// pressEdit opens the document under the cursor in your editor, or saves and closes the one open.
func (d *Screen) pressEdit(in screen.Inputs) screen.Request {
	if d.editing != nil {
		return d.closeEdit()
	}

	item, ok := d.hovered(in)
	if !ok {
		return nil
	}

	read, asked := d.bodies[item.id]
	if !asked || read.err != nil || read.body == "" || read.pending || read.updatedAt.Before(item.updatedAt) {
		return screen.Note{
			Kind:    screen.Error,
			Summary: "could not edit " + item.name,
			Detail:  "its text has not been read yet",
		}
	}

	path, err := writeCopy(d.deps.DraftDir, read.body)
	if err != nil {
		return screen.Report{Summary: "could not edit " + item.name, Err: err}
	}

	return screen.Stage{
		Content: editor.Content(d.deps.EditorCommand, filepath.Dir(path), path, 1),
		Staged: func(pane string) tea.Msg {
			return editorStaged{item: item, read: read, path: path, pane: pane}
		},
	}
}

// editorStaged is the pane an editor over a document's copy was given, empty where the
// split was refused.
type editorStaged struct {
	item document
	read text
	path string
	pane string
}

// applyEditorStaged opens the edit in the editor's pane, or throws the copy away when there was no
// room for one.
func (d *Screen) applyEditorStaged(staged editorStaged) screen.Request {
	item, read, path, pane := staged.item, staged.read, staged.path, staged.pane
	if pane == "" {
		return screen.Batch{
			screen.Report{Summary: "could not edit " + item.name, Err: removeCopy(path)},
			screen.Note{
				Kind:    screen.Error,
				Summary: "could not edit " + item.name,
				Detail:  "there is no room beside the sidebar for an editor",
			},
		}
	}

	d.history = nil
	d.editing = &editing{
		item:    item,
		version: read.version,
		path:    path,
		pane:    pane,
		body:    read.body,
	}

	return nil
}

// closeEdit takes the editor down and writes back what changed in the copy.
func (d *Screen) closeEdit() screen.Request {
	if d.editing == nil {
		return nil
	}

	edit := *d.editing
	d.editing = nil

	return screen.Batch{screen.Unstage{}, d.saveEdit(edit)}
}

// saveEdit writes back what changed in the copy.
func (d *Screen) saveEdit(edit editing) screen.Request {
	summary := "could not save " + edit.item.name
	content, err := os.ReadFile(edit.path)
	if err != nil {
		return screen.Report{Summary: summary, Err: fmt.Errorf("failed to read your copy at %s: %w", edit.path, err)}
	}

	body := string(content)
	if strings.TrimSpace(body) == "" {
		return screen.Note{
			Kind:    screen.Error,
			Summary: summary,
			Detail:  "komrad does not save an empty document, so your copy stays at " + edit.path,
		}
	}

	if body == edit.body {
		return screen.Report{Summary: summary, Err: removeCopy(edit.path)}
	}

	return d.deps.Clients.Write("saving "+edit.item.name, summary, func(a api.Clients) error {
		if err := a.WriteDocument(api.WriteDocumentParams{
			ID:      edit.item.id,
			Version: edit.version,
			Name:    edit.item.name,
			Type:    edit.item.typeName,
			Body:    body,
		}); err != nil {
			return fmt.Errorf("failed to save, so your copy is left at %s: %w", edit.path, err)
		}

		return removeCopy(edit.path)
	})
}

// ReconcileEditor catches the editor pane going away on its own and saves what it wrote.
func (d *Screen) ReconcileEditor(in screen.Inputs) screen.Request {
	if d.editing == nil || d.deps.Panes.IsPaneAlive(d.editing.pane) {
		return nil
	}

	return d.closeEdit()
}

// editLabel says what the edit entry would do.
func (d *Screen) editLabel() string {
	if d.editing != nil {
		return "close editor"
	}

	return "edit"
}

func writeCopy(dir, body string) (string, error) {
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

func removeCopy(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove your copy at %s: %w", path, err)
	}

	return nil
}
