package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"komrad/internal/ui/form"

	tea "charm.land/bubbletea/v2"
)

// formFieldEditedMsg is what the editor wrote for one field of the form on screen.
type formFieldEditedMsg struct {
	// value is the string the field was opened on.
	value *string
	text  string
	err   error
}

// openFormFieldEditor opens the editor on the field with the cursor, and reports whether the
// key belonged to the panel.
func (m *model) openFormFieldEditor() (tea.Cmd, bool) {
	focused := m.form.rendered.GetFocusedField()
	if focused == nil {
		return nil, false
	}

	key := focused.GetKey()
	index := slices.IndexFunc(m.form.Editable, func(field form.Editable) bool { return field.Key == key })
	if index < 0 {
		return nil, false
	}

	value := m.form.Editable[index].Value
	path := formFieldDraftPath(key)
	if err := os.WriteFile(path, []byte(*value), 0o600); err != nil {
		m.report("Your editor could not be opened on that field", err)

		return nil, true
	}

	// The field is a file with nothing above it, so the editor opens on line one.
	//nolint:noctx // the editor runs until the field is closed, so it takes no deadline
	open := exec.Command("sh", "-c", editorShell(m.settings.Editor, path, 1))

	return tea.ExecProcess(open, func(err error) tea.Msg {
		if err != nil {
			return formFieldEditedMsg{
				value: value,
				err:   err,
			}
		}

		text, err := os.ReadFile(path)

		return formFieldEditedMsg{
			value: value,
			text:  string(text),
			err:   err,
		}
	}), true
}

// formFieldDraftPath is the file the panel writes a field to for the editor to open.
func formFieldDraftPath(key string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("komrad-field-%s-%d.md", key, os.Getpid()))
}

// applyFormFieldEdited writes what the editor wrote back into the field it was opened on.
func (m *model) applyFormFieldEdited(msg formFieldEditedMsg) tea.Cmd {
	index := slices.IndexFunc(m.form.Editable, func(field form.Editable) bool { return field.Value == msg.value })
	if !m.form.isActive() || index < 0 {
		return nil
	}
	if msg.err != nil {
		m.report("Your editor could not be opened on that field", msg.err)

		return nil
	}

	*msg.value = strings.TrimRight(msg.text, "\n")

	return m.rerenderFormOn(m.form.Editable[index].Key)
}

// rerenderFormOn renders the form again and leaves the cursor on the field named.
func (m *model) rerenderFormOn(key string) tea.Cmd {
	cmd := m.rebuildForm()
	if cmd == nil {
		return nil
	}

	return tea.Batch(append([]tea.Cmd{cmd}, m.focusFieldCmds(key)...)...)
}

// focusFieldCmds steps the form's cursor from its first field to the field named.
func (m *model) focusFieldCmds(key string) []tea.Cmd {
	var cmds []tea.Cmd
	for range max(slices.Index(m.form.order, key), 0) {
		cmds = append(cmds, m.form.rendered.NextField())
	}

	return cmds
}
