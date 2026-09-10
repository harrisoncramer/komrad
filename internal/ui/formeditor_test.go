package ui

import (
	"os"
	"testing"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEditorKeyOpensTheFieldUnderTheCursor covers the panel taking the editor key itself, since
// huh hands what its own editor wrote to every text field of the group.
func TestEditorKeyOpensTheFieldUnderTheCursor(t *testing.T) {
	m := formModel(&formRecorder{})
	m.editor = "true"

	values := workflowFormValues{
		Context:              "the notes as they stand",
		WorktreeCopyPaths:    ".env",
		WorktreeSymlinkPaths: "node_modules",
		WorktreeDiffExclude:  "proto/gen",
	}

	opened, _ := m.openWorkflowConfigForm(&values, nil)
	m = asModel(t, opened)
	m.form.form.NextField()

	require.Equal(t, "context", m.form.form.GetFocusedField().GetKey(), "the cursor is on the field the editor opens")

	edited, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = asModel(t, edited)
	require.NotNil(t, cmd, "the editor key is the panel's own, so it is answered rather than handed to huh")

	draft, err := os.ReadFile(formFieldDraftPath("context"))
	require.NoError(t, err, "the field is written out for the editor to open")
	t.Cleanup(func() { _ = os.Remove(formFieldDraftPath("context")) })
	assert.Equal(t, "the notes as they stand", string(draft))

	written, _ := m.Update(formFieldEditedMsg{kind: formWorkflow, key: "context", text: "the notes as the editor left them"})
	m = asModel(t, written)

	assert.Equal(t, "the notes as the editor left them", values.Context)
	assert.Equal(t, ".env", values.WorktreeCopyPaths, "a field the editor was not opened on keeps what it held")
	assert.Equal(t, "node_modules", values.WorktreeSymlinkPaths)
	assert.Equal(t, "proto/gen", values.WorktreeDiffExclude)

	assert.Contains(t, m.form.form.View(), "the notes as the editor left them", "the field shows what the editor wrote")
	assert.Equal(t, "context", m.form.form.GetFocusedField().GetKey(), "the cursor is left where the editor was opened")
}

// TestEditorKeyIsHandedOnByAFieldWithNoText covers a form the panel has no editor for, which is
// every form asking only for a line or an answer.
func TestEditorKeyIsHandedOnByAFieldWithNoText(t *testing.T) {
	m := formModel(&formRecorder{})
	m.editor = "true"

	name := ""
	opened, _ := m.openRenameForm(agentstore.Agent{ID: "a1", Name: "api"}, &name)
	m = asModel(t, opened)

	_, _, taken := m.openFormFieldEditor()
	assert.False(t, taken, "there is no text field to open, so the key belongs to the form")

	_, err := os.Stat(formFieldDraftPath("name"))
	assert.True(t, os.IsNotExist(err), "nothing is written out for a field the panel does not edit")
}
