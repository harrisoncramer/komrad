package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// editableText is a form's text field and the string it is drawn from.
type editableText struct {
	key   string
	value *string
}

// formFieldEditedMsg is what the editor wrote for one field of the form on screen.
type formFieldEditedMsg struct {
	kind formKind
	key  string
	text string
	err  error
}

// openFormFieldEditor opens the editor on the field with the cursor, and reports whether the
// key belonged to the panel.
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

	// The field is a file with nothing above it, so the editor opens on line one.
	//nolint:noctx // the editor runs until the field is closed, so it takes no deadline
	open := exec.Command("sh", "-c", editorShell(m.settings.Editor, path, 1))

	return m, tea.ExecProcess(open, func(err error) tea.Msg {
		if err != nil {
			return formFieldEditedMsg{
				kind: kind,
				key:  key,
				err:  err,
			}
		}

		text, err := os.ReadFile(path)

		return formFieldEditedMsg{
			kind: kind,
			key:  key,
			text: string(text),
			err:  err,
		}
	}), true
}

// formFieldDraftPath is the file a field is written to for the editor to open.
func formFieldDraftPath(key string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("weave-field-%s-%d.md", key, os.Getpid()))
}

// applyFormFieldEdited writes what the editor wrote back into the field it was opened on.
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

	*m.form.editable[index].value = strings.TrimRight(msg.text, "\n")

	return m, m.redrawFormOn(msg.key)
}

// redrawFormOn draws the form again and leaves the cursor on the field named.
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
