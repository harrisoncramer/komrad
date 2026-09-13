package ui

import (
	"os"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEditorKeyOpensTheFieldUnderTheCursor checks that an edit reaches the field with the cursor
// and no other field.
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

	written, _ := m.Update(formFieldEditedMsg{
		kind: formWorkflow,
		key:  "context",
		text: "the notes as the editor left them\n",
	})
	m = asModel(t, written)

	assert.Equal(t, "the notes as the editor left them", values.Context, "the newline every editor writes at the end of a file is not part of the field")
	assert.Equal(t, ".env", values.WorktreeCopyPaths, "a field the editor was not opened on keeps what it held")
	assert.Equal(t, "node_modules", values.WorktreeSymlinkPaths)
	assert.Equal(t, "proto/gen", values.WorktreeDiffExclude)

	assert.Contains(t, m.form.form.View(), "the notes as the editor left them", "the field shows what the editor wrote")
	assert.Equal(t, "context", m.form.form.GetFocusedField().GetKey(), "the cursor is left where the editor was opened")
}

// TestEditorKeyIsHandedOnByAFieldWithNoText checks that a form with nothing to edit leaves the
// key to huh.
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

// TestEditorKeyOnEitherProvisioner checks the cursor lands on the edited field under either
// provisioner.
func TestEditorKeyOnEitherProvisioner(t *testing.T) {
	tests := []struct {
		name        string
		provisioner string
		field       string
		want        func(*workflowFormValues) string
	}{
		{
			name:        "a list field of a workflow that makes worktrees",
			provisioner: provisionerWorktree,
			field:       "worktree_copy_paths",
			want:        func(values *workflowFormValues) string { return values.WorktreeCopyPaths },
		},
		{
			name:        "the context of a workflow that provisions nothing",
			provisioner: provisionerNone,
			field:       "context",
			want:        func(values *workflowFormValues) string { return values.Context },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := formModel(&formRecorder{})
			m.editor = "true"

			values := workflowFormValues{Provisioner: tt.provisioner, Context: "the notes"}

			opened, _ := m.openWorkflowConfigForm(&values, nil)
			m = asModel(t, opened)

			written, _ := m.Update(formFieldEditedMsg{
				kind: formWorkflow,
				key:  tt.field,
				text: "what the editor wrote",
			})
			m = asModel(t, written)

			assert.Equal(t, "what the editor wrote", tt.want(&values))
			assert.Equal(t, tt.field, m.form.form.GetFocusedField().GetKey(), "the cursor is left where the editor was opened")
		})
	}
}

// TestFormOrderLeavesOutANote checks the cursor steps the same way huh does, huh passing a note
// by rather than focusing it.
func TestFormOrderLeavesOutANote(t *testing.T) {
	prompt, title := "", ""

	form, order := formOf(config.FixedFormKeys(),
		huh.NewNote().Description("nothing to answer here"),
		textField("prompt").Value(&prompt),
		huh.NewInput().Key("title").Value(&title),
	)
	form.Init()

	assert.Equal(t, []string{"prompt", "title"}, order, "a note takes no cursor stop, so it takes no place in the order")
	assert.Equal(t, "prompt", form.GetFocusedField().GetKey(), "the form opens on the first field the cursor can reach")

	form.NextField()
	assert.Equal(t, "title", form.GetFocusedField().GetKey(), "one step is one field the cursor can reach")
}
