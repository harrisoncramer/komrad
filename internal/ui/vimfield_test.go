package ui_test

import (
	"slices"
	"strings"
	"testing"

	"komrad/internal/ui"
	"komrad/internal/ui/paneltest"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVimEditing covers typing into a form's text fields with vim_mode on.
func TestVimEditing(t *testing.T) {
	t.Run("with vim off one esc closes the form", func(t *testing.T) {
		harness := openForms(t, paneltest.FormWorkflow(), paneltest.FormAuthor())

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("go", ""), []string{"esc"})...)

		assert.NotContains(t, harness.Frame(), "Brief")
	})

	t.Run("with vim on the first esc goes to normal mode and the second closes the form", func(t *testing.T) {
		harness := openVimForms(t)

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("go", ""), []string{"esc"})...)
		assert.Contains(t, harness.Frame(), "Brief")

		harness.Press("esc")
		assert.NotContains(t, harness.Frame(), "Brief")
	})

	t.Run("with vim on the second esc in a subform goes back to the top form", func(t *testing.T) {
		harness := openVimForms(t)

		harness.Press(slices.Concat([]string{"space", "w"}, strings.Split("web", ""), openWorkflowSubformKeys(0), strings.Split("codex", ""), []string{"esc", "esc"})...)

		frame := harness.Frame()
		assert.NotContains(t, frame, "Default Agent")
		assert.Contains(t, frame, "New workflow name")
	})

	t.Run("ctrl+[ counts as esc in a form", func(t *testing.T) {
		harness := openVimForms(t)

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("go", ""), []string{"ctrl+["})...)
		assert.Contains(t, harness.Frame(), "Brief")

		harness.Press("ctrl+[")
		assert.NotContains(t, harness.Frame(), "Brief")
	})

	t.Run("an edit made in normal mode is what the form sends", func(t *testing.T) {
		harness := openVimForms(t)

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("check CI", ""), []string{"esc", "c", "i", "w"}, strings.Split("it", ""),
			[]string{"esc", "0", "~", "enter"})...)

		sent := harness.Sent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "Check it", create.GetPrompt())
	})

	t.Run("the field's own editing keys do nothing in normal mode", func(t *testing.T) {
		harness := openVimForms(t)

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("go far", ""), []string{"esc", "ctrl+w", "ctrl+u", "enter"})...)

		sent := harness.Sent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "go far", create.GetPrompt())
	})

	t.Run("k on the first field stays on it", func(t *testing.T) {
		harness := openVimForms(t)

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("go", ""), []string{"esc", "k", "x", "enter"})...)

		sent := harness.Sent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "g", create.GetPrompt())
	})

	t.Run("j above only fields that are not text stays on the field", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{Settings: ui.PanelSettings{VimMode: true}})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(paneltest.ConfiguredWorkflow(), paneltest.FormAuthor()))
		harness.Press("g", "g")

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("go", ""), slices.Repeat([]string{"tab"}, 2), strings.Split("ab", ""), []string{"esc", "j"})...)
		require.Empty(t, harness.Sent(agentv1connect.AgentServiceCreateAgentProcedure))
		assert.Contains(t, harness.Frame(), "Worktree")

		harness.Press("x", "enter")

		sent := harness.Sent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "a", create.GetFlags())
	})

	t.Run("u takes back what the editor wrote into the field", func(t *testing.T) {
		harness := paneltest.Start(t, paneltest.Params{Settings: ui.PanelSettings{VimMode: true}})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(paneltest.ConfiguredWorkflow(), paneltest.FormAuthor()))
		harness.Press("g", "g")
		t.Cleanup(func() { removeEdited(harness) })

		harness.WriteInEditor("what the editor wrote\n")
		harness.Press("space", "E", "tab", "esc", "ctrl+e")
		harness.Press("u", "enter")

		sent := harness.Sent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure)
		require.Len(t, sent, 1)
		update, ok := sent[0].(*workflowv1.UpdateWorkflowRequest)
		require.True(t, ok)
		assert.Equal(t, "shared notes", update.GetContext())
	})

	t.Run("a normal-mode edit in a form is rendered at once", func(t *testing.T) {
		harness := openVimForms(t)

		harness.Press(slices.Concat([]string{"space", "c"}, strings.Split("still here", ""), []string{"esc", "b", "d", "w"})...)

		frame := harness.Frame()
		assert.Contains(t, frame, "still")
		assert.NotContains(t, frame, "still here")
	})
}

// TestVimLineEditing covers vim_mode in the one-row fields: the filter, the search line and a rename.
func TestVimLineEditing(t *testing.T) {
	tests := []struct {
		name       string
		press      []string
		wantSearch string
	}{
		{
			name:       "esc gives the filter up with vim on",
			press:      []string{"/", "a", "b", "esc"},
			wantSearch: "/",
		},
		{
			name:       "ctrl+f puts the filter in normal mode and keeps the pattern",
			press:      []string{"/", "a", "b", "ctrl+f"},
			wantSearch: "/ a[b]",
		},
		{
			name:       "esc in normal mode gives the filter up",
			press:      []string{"/", "a", "b", "ctrl+f", "esc"},
			wantSearch: "/",
		},
		{
			name:       "a word changed in normal mode is the filter's pattern",
			press:      slices.Concat([]string{"/"}, strings.Split("db api", ""), []string{"ctrl+f", "c", "i", "w"}, strings.Split("ui", "")),
			wantSearch: "/ db ui▏",
		},
		{
			name:       "esc after a change in normal mode goes back to normal mode",
			press:      slices.Concat([]string{"/"}, strings.Split("db api", ""), []string{"ctrl+f", "c", "i", "w"}, strings.Split("ui", ""), []string{"esc"}),
			wantSearch: "/ db u[i]",
		},
		{
			name:       "enter in normal mode applies the filter",
			press:      []string{"/", "d", "b", "ctrl+f", "enter"},
			wantSearch: "/ db",
		},
		{
			name:       "ctrl+f puts the search line in normal mode",
			press:      []string{"tab", "/", "f", "o", "o", "ctrl+f"},
			wantSearch: "/ fo[o]",
		},
		{
			name:       "a word deleted in normal mode leaves the search line",
			press:      slices.Concat([]string{"tab", "/"}, strings.Split("foo bar", ""), []string{"ctrl+f", "0", "d", "w"}),
			wantSearch: "/ [b]ar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openVimForms(t)

			harness.Press(tt.press...)

			assert.Equal(t, tt.wantSearch, paneltest.Search(harness.TypedFrame()))
		})
	}

	t.Run("a name changed in normal mode is the name a rename sends", func(t *testing.T) {
		harness := openVimForms(t)

		harness.Press("j", "r", "ctrl+f", "0", "~", "enter")

		harness.AssertSentTo(agentv1connect.AgentServiceUpdateAgentProcedure, &agentv1.UpdateAgentRequest{Id: "a1", Name: "Author"})
	})

	t.Run("esc cancels a rename with vim on", func(t *testing.T) {
		harness := openVimForms(t)

		harness.Press("j", "r", "x", "esc", "enter")

		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceUpdateAgentProcedure))
	})
}

// TestVimEditingBadge covers the mode rendered at the foot of the sidebar while a text field holds the keys.
func TestVimEditingBadge(t *testing.T) {
	tests := []struct {
		name  string
		vim   bool
		press []string
		want  string
	}{
		{
			name:  "the list is in normal mode",
			press: nil,
			want:  "NORMAL",
		},
		{
			name:  "a form's text field is in edit mode with vim off",
			press: []string{"space", "c"},
			want:  "EDIT",
		},
		{
			name:  "a form's text field opens in edit mode with vim on",
			vim:   true,
			press: []string{"space", "c"},
			want:  "  EDIT  ",
		},
		{
			name:  "esc puts a form's text field in normal mode",
			vim:   true,
			press: []string{"space", "c", "esc"},
			want:  " NORMAL ",
		},
		{
			name:  "i puts it back in insert mode",
			vim:   true,
			press: []string{"space", "c", "esc", "i"},
			want:  "  EDIT  ",
		},
		{
			name:  "the list's filter is in edit mode",
			press: []string{"/"},
			want:  "EDIT",
		},
		{
			name:  "the search line is in edit mode",
			press: []string{"tab", "/"},
			want:  "EDIT",
		},
		{
			name:  "a rename is in edit mode",
			press: []string{"j", "r"},
			want:  "EDIT",
		},
		{
			name:  "ctrl+f puts the filter in normal mode",
			vim:   true,
			press: []string{"/", "a", "ctrl+f"},
			want:  " NORMAL ",
		},
		{
			name:  "ctrl+f puts the search line in normal mode",
			vim:   true,
			press: []string{"tab", "/", "a", "ctrl+f"},
			want:  " NORMAL ",
		},
		{
			name:  "ctrl+f puts a rename in normal mode",
			vim:   true,
			press: []string{"j", "r", "ctrl+f"},
			want:  " NORMAL ",
		},
		{
			name:  "i puts the filter back in insert mode",
			vim:   true,
			press: []string{"/", "a", "ctrl+f", "i"},
			want:  "  EDIT  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.Start(t, paneltest.Params{Settings: ui.PanelSettings{VimMode: tt.vim}})
			harness.AnswerWorktreeReads()
			harness.Watch(paneltest.Snapshot(paneltest.FormWorkflow(), paneltest.FormAuthor()))
			harness.Press("g", "g")
			harness.Press(tt.press...)

			assert.Contains(t, harness.Frame(), tt.want)
		})
	}
}

func openVimForms(t *testing.T) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{Settings: ui.PanelSettings{VimMode: true}})
	harness.AnswerWorktreeReads()
	harness.Watch(paneltest.Snapshot(paneltest.FormWorkflow(), paneltest.FormAuthor()))
	harness.Press("g", "g")

	return harness
}
