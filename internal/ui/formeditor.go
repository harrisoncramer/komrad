package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	tea "github.com/charmbracelet/bubbletea"
)

// editableText is one of a form's text fields as the panel's editor key sees it.
type editableText struct {
	key   string
	value *string
}

// formFieldEditedMsg carries what the editor wrote for one field of the form on screen.
type formFieldEditedMsg struct {
	kind formKind
	key  string
	text string
	err  error
}

// Explainer: huh's own editor hands what it wrote to the whole group as one message, and every
// text field in that group takes it, so a form with several of them loses the rest to whichever
// one was edited. The panel opens the editor itself instead, on the field with the cursor.

// openFormFieldEditor hands the field under the cursor to the editor, and reports whether the
// key was the panel's to answer.
func (m model) openFormFieldEditor() (tea.Model, tea.Cmd, bool) {
	focused := m.form.form.GetFocusedField()
	if focused == nil {
		return m, nil, false
	}

	key := focused.GetKey()
	index := slices.IndexFunc(m.form.editable, func(field editableText) bool { return field.key == key })
	if index < 0 {
		return m, nil, false
	}

	field := m.form.editable[index]
	path := formFieldDraftPath(key)
	if err := os.WriteFile(path, []byte(*field.value), 0o600); err != nil {
		m.report("Your editor could not be opened on that field", err)

		return m, nil, true
	}

	kind := m.form.kind

	// The field is one file with nothing above it, so the editor opens on its first line.
	//nolint:noctx // the editor runs until the field is closed, so it takes no deadline
	open := exec.Command("sh", "-c", editorShell(m.editor, path, 1))

	return m, tea.ExecProcess(open, func(err error) tea.Msg {
		if err != nil {
			return formFieldEditedMsg{kind: kind, key: key, err: err}
		}

		text, err := os.ReadFile(path)

		return formFieldEditedMsg{kind: kind, key: key, text: string(text), err: err}
	}), true
}

// formFieldDraftPath is where a field is put for the editor to open.
func formFieldDraftPath(key string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("weave-field-%s-%d.md", key, os.Getpid()))
}

// applyFormFieldEdited puts what the editor wrote back into the field it was opened on.
func (m model) applyFormFieldEdited(msg formFieldEditedMsg) (tea.Model, tea.Cmd) {
	if !m.form.isActive() || m.form.kind != msg.kind {
		return m, nil
	}
	if msg.err != nil {
		m.report("Your editor could not be opened on that field", msg.err)

		return m, nil
	}

	index := slices.IndexFunc(m.form.editable, func(field editableText) bool { return field.key == msg.key })
	if index < 0 {
		return m, nil
	}

	*m.form.editable[index].value = msg.text

	return m, m.redrawFormOn(msg.key)
}

// redrawFormOn draws the form again around the values it edits, since a field shows the text it
// was built with, and leaves the cursor on the field named.
func (m *model) redrawFormOn(key string) tea.Cmd {
	cmd := m.rebuildForm()
	if cmd == nil {
		return nil
	}

	cmds := []tea.Cmd{cmd}
	for range max(slices.Index(m.form.order, key), 0) {
		cmds = append(cmds, m.form.form.NextField())
	}

	return tea.Batch(cmds...)
}
