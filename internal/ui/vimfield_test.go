package ui_test

import (
	"os"
	"slices"
	"testing"

	"komrad/internal/gittest"
	"komrad/internal/ui"
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
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go"), []string{"esc"})...)

		assert.NotContains(t, harness.readFrame(), "Initial prompt")
	})

	t.Run("with vim on the first esc goes to normal mode and the second closes the form", func(t *testing.T) {
		harness := openVimForms(t)

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go"), []string{"esc"})...)
		assert.Contains(t, harness.readFrame(), "Initial prompt")

		harness.press("esc")
		assert.NotContains(t, harness.readFrame(), "Initial prompt")
	})

	t.Run("ctrl+[ counts as esc in a form", func(t *testing.T) {
		harness := openVimForms(t)

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go"), []string{"ctrl+["})...)
		assert.Contains(t, harness.readFrame(), "Initial prompt")

		harness.press("ctrl+[")
		assert.NotContains(t, harness.readFrame(), "Initial prompt")
	})

	t.Run("ctrl+[ counts as esc in the review box", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{settings: ui.PanelSettings{VimMode: true}})

		harness.press("c")
		typeText(harness, "gone")
		harness.press("ctrl+[")
		assert.Contains(t, harness.readFrame(), "  Comment")

		harness.press("ctrl+[")
		assert.NotContains(t, harness.readFrame(), "  Comment")
	})

	t.Run("an edit made in normal mode is what the form sends", func(t *testing.T) {
		harness := openVimForms(t)

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("check CI"), []string{"esc", "c", "i", "w"}, spellKeys("it"),
			[]string{"esc", "0", "~", "ctrl+s"})...)

		sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "Check it", create.GetPrompt())
	})

	t.Run("the field's own editing keys do nothing in normal mode", func(t *testing.T) {
		harness := openVimForms(t)

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go far"), []string{"esc", "ctrl+w", "ctrl+u", "ctrl+s"})...)

		sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "go far", create.GetPrompt())
	})

	t.Run("j in normal mode steps over a field that is not text and arrives in normal mode", func(t *testing.T) {
		harness := openVimForms(t)

		harness.press(slices.Concat([]string{"space", "w"}, spellKeys("web"), slices.Repeat([]string{"tab"}, 6), []string{"esc", "j", "i"},
			spellKeys("main"), []string{"esc", "ctrl+s"})...)

		sent := harness.readSent(workflowv1connect.WorkflowServiceCreateWorkflowProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*workflowv1.CreateWorkflowRequest)
		require.True(t, ok)
		assert.Equal(t, "main", create.GetWorktree().GetBaseBranch())
	})

	t.Run("k on the first field stays on it", func(t *testing.T) {
		harness := openVimForms(t)

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go"), []string{"esc", "k", "x", "ctrl+s"})...)

		sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "g", create.GetPrompt())
	})

	t.Run("j above only fields that are not text stays on the field", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{settings: ui.PanelSettings{VimMode: true}})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newConfiguredWorkflow(), newFormAuthor()))
		harness.press("g", "g")

		harness.press(slices.Concat([]string{"space", "c"}, spellKeys("go"), slices.Repeat([]string{"tab"}, 4), spellKeys("ab"), []string{"esc", "j"})...)
		require.Empty(t, harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure))
		assert.Contains(t, harness.readFrame(), "Worktree")

		harness.press("x", "ctrl+s")

		sent := harness.readSent(agentv1connect.AgentServiceCreateAgentProcedure)
		require.Len(t, sent, 1)
		create, ok := sent[0].(*agentv1.CreateAgentRequest)
		require.True(t, ok)
		assert.Equal(t, "a", create.GetFlags())
	})

	t.Run("u takes back what the editor wrote into the field", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{settings: ui.PanelSettings{Editor: "true", VimMode: true}})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newConfiguredWorkflow(), newFormAuthor()))
		harness.press("g", "g")
		t.Cleanup(func() { _ = os.Remove(ui.FormFieldDraftPath("context")) })

		harness.press("space", "E", "tab", "esc", "ctrl+e")
		harness.send(harness.panel.NewFieldEdited("context", "what the editor wrote\n"))
		harness.press("u", "ctrl+s")

		sent := harness.readSent(workflowv1connect.WorkflowServiceUpdateWorkflowProcedure)
		require.Len(t, sent, 1)
		update, ok := sent[0].(*workflowv1.UpdateWorkflowRequest)
		require.True(t, ok)
		assert.Equal(t, "shared notes", update.GetContext())
	})

	t.Run("a comment edited in normal mode is what the review posts", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{settings: ui.PanelSettings{VimMode: true}})

		harness.press("c")
		typeText(harness, "still here")
		harness.press("esc", "b", "d", "w", "ctrl+s")

		harness.assertSentTo(agentv1connect.AgentServiceCreateReviewThreadProcedure, &agentv1.CreateReviewThreadRequest{
			AgentId:     "a1",
			FilePath:    gittest.File,
			OldStart:    1,
			OldEnd:      1,
			NewStart:    1,
			NewEnd:      1,
			QuotedLines: []string{" package greet"},
			Body:        "still",
			BaseSha:     gittest.BaseSHA,
			SnapshotSha: gittest.SnapshotSHA,
		})
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
			press:      slices.Concat([]string{"/"}, spellKeys("db api"), []string{"ctrl+f", "c", "i", "w"}, spellKeys("ui")),
			wantSearch: "/ db ui▏",
		},
		{
			name:       "esc after a change in normal mode goes back to normal mode",
			press:      slices.Concat([]string{"/"}, spellKeys("db api"), []string{"ctrl+f", "c", "i", "w"}, spellKeys("ui"), []string{"esc"}),
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
			name:       "the search line opened again types after its text",
			press:      []string{"tab", "/", "f", "o", "o", "ctrl+f", "esc", "/", "d"},
			wantSearch: "/ food▏",
		},
		{
			name:       "a word deleted in normal mode leaves the search line",
			press:      slices.Concat([]string{"tab", "/"}, spellKeys("foo bar"), []string{"ctrl+f", "0", "d", "w"}),
			wantSearch: "/ [b]ar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openVimForms(t)

			harness.press(tt.press...)

			assert.Equal(t, tt.wantSearch, readSearch(harness.readTypedFrame()))
		})
	}

	t.Run("a name changed in normal mode is the name a rename sends", func(t *testing.T) {
		harness := openVimForms(t)

		harness.press("j", "r", "ctrl+f", "0", "~", "enter")

		harness.assertSentTo(agentv1connect.AgentServiceUpdateAgentProcedure, &agentv1.UpdateAgentRequest{Id: "a1", Name: "Author"})
	})

	t.Run("esc cancels a rename with vim on", func(t *testing.T) {
		harness := openVimForms(t)

		harness.press("j", "r", "x", "esc", "enter")

		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceUpdateAgentProcedure))
	})
}

// TestVimEditingBadge covers the mode drawn at the foot of the sidebar while a text field holds the keys.
func TestVimEditingBadge(t *testing.T) {
	tests := []struct {
		name  string
		vim   bool
		press []string
		want  string
	}{
		{name: "the list is in normal mode", press: nil, want: "NORMAL"},
		{name: "a form's text field is in edit mode with vim off", press: []string{"space", "c"}, want: "EDIT"},
		{name: "a form's text field opens in edit mode with vim on", vim: true, press: []string{"space", "c"}, want: "  EDIT  "},
		{name: "esc puts a form's text field in normal mode", vim: true, press: []string{"space", "c", "esc"}, want: " NORMAL "},
		{name: "i puts it back in insert mode", vim: true, press: []string{"space", "c", "esc", "i"}, want: "  EDIT  "},
		{name: "the list's filter is in edit mode", press: []string{"/"}, want: "EDIT"},
		{name: "the search line is in edit mode", press: []string{"tab", "/"}, want: "EDIT"},
		{name: "a rename is in edit mode", press: []string{"j", "r"}, want: "EDIT"},
		{name: "ctrl+f puts the filter in normal mode", vim: true, press: []string{"/", "a", "ctrl+f"}, want: " NORMAL "},
		{name: "ctrl+f puts the search line in normal mode", vim: true, press: []string{"tab", "/", "a", "ctrl+f"}, want: " NORMAL "},
		{name: "ctrl+f puts a rename in normal mode", vim: true, press: []string{"j", "r", "ctrl+f"}, want: " NORMAL "},
		{name: "i puts the filter back in insert mode", vim: true, press: []string{"/", "a", "ctrl+f", "i"}, want: "  EDIT  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{settings: ui.PanelSettings{VimMode: tt.vim}})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(newFormWorkflow(), newFormAuthor()))
			harness.press("g", "g")
			harness.press(tt.press...)

			assert.Contains(t, harness.readFrame(), tt.want)
		})
	}

	t.Run("the review comment box is in edit mode", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{settings: ui.PanelSettings{VimMode: true}})

		harness.press("c")
		assert.Contains(t, harness.readFrame(), "  EDIT  ")

		harness.press("esc")
		assert.Contains(t, harness.readFrame(), " NORMAL ")
	})
}

func openVimForms(t *testing.T) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{settings: ui.PanelSettings{VimMode: true}})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newFormWorkflow(), newFormAuthor()))
	harness.press("g", "g")

	return harness
}
