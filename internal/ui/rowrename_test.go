package ui_test

import (
	"errors"
	"slices"
	"testing"

	"komrad/internal/ui/paneltest"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
	"komrad/proto/gen/workflow/v1/workflowv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestRenameSends covers a name typed over its row going out as the write the server renames by.
func TestRenameSends(t *testing.T) {
	tests := []struct {
		name      string
		press     []string
		procedure string
		want      []proto.Message
	}{
		{
			name:      "an agent's name is typed onto the one it had",
			press:     slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"enter"}),
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
			want:      []proto.Message{&agentv1.UpdateAgentRequest{Id: "a1", Name: "author-v2"}},
		},
		{
			name:      "keys bound elsewhere are typed into the name",
			press:     slices.Concat([]string{"j", "r"}, spellKeys("jkq"), []string{"enter"}),
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
			want:      []proto.Message{&agentv1.UpdateAgentRequest{Id: "a1", Name: "authorjkq"}},
		},
		{
			name:      "backspace takes the name back a letter at a time",
			press:     slices.Concat([]string{"j", "r"}, slices.Repeat([]string{"backspace"}, 6), spellKeys("writer"), []string{"enter"}),
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
			want:      []proto.Message{&agentv1.UpdateAgentRequest{Id: "a1", Name: "writer"}},
		},
		{
			name:      "home moves the cursor to the start of the name",
			press:     slices.Concat([]string{"j", "r", "home"}, spellKeys("co-"), []string{"enter"}),
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
			want:      []proto.Message{&agentv1.UpdateAgentRequest{Id: "a1", Name: "co-author"}},
		},
		{
			name:      "a workflow renamed on its header carries the config it is not changing",
			press:     slices.Concat([]string{"r"}, slices.Repeat([]string{"backspace"}, 3), spellKeys("backend"), []string{"enter"}),
			procedure: workflowv1connect.WorkflowServiceUpdateWorkflowProcedure,
			want: []proto.Message{&workflowv1.UpdateWorkflowRequest{
				Id:       "w1",
				Name:     "backend",
				Cwd:      "/work/api",
				Flags:    "--model opus",
				Harness:  "claude",
				Worktree: &workflowv1.WorktreeConfig{},
			}},
		},
		{
			name:      "esc sends nothing",
			press:     slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"esc"}),
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
		},
		{
			name:      "the name it had sends nothing",
			press:     []string{"j", "r", "space", "enter"},
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
		},
		{
			name:      "a blank name sends nothing",
			press:     slices.Concat([]string{"j", "r"}, slices.Repeat([]string{"backspace"}, 6), []string{"space", "enter"}),
			procedure: agentv1connect.AgentServiceUpdateAgentProcedure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openForms(t, newFormWorkflow(), newFormAuthor())

			harness.Press(tt.press...)

			harness.AssertSentTo(tt.procedure, tt.want...)
			assert.NotContains(t, paneltest.ListRows(harness.Frame()), "  author-v2▏", "the field has closed")
		})
	}
}

// TestRenameField covers the name being typed over its row, rendered where the name was.
func TestRenameField(t *testing.T) {
	t.Run("the field opens on the name the row had", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press("j", "r")

		frame := harness.Frame()
		assert.Equal(t, "└─ author▏", paneltest.HoveredRow(frame))
		assert.Contains(t, paneltest.Legend(frame), "enter save")
		assert.Contains(t, paneltest.Legend(frame), "esc cancel")
		assert.NotContains(t, frame, "enter submit", "no form opens")
		assert.NotContains(t, paneltest.Legend(frame), "menu", "space is typed, not the menu")
	})

	t.Run("a paste goes in on one line", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press("j", "r")
		harness.Send(tea.PasteMsg{Content: "-a\nb"})

		assert.Equal(t, "└─ author-a b▏", paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("esc puts the name back and the keys go back to the list", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"esc", "k"})...)

		assert.Equal(t, "api", paneltest.HoveredRow(harness.Frame()))
		assert.Contains(t, paneltest.ListRows(harness.Frame()), "└─ author")
	})

	t.Run("a row that leaves the list holds the keys until esc or enter and says it is gone", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"))...)

		harness.Watch(paneltest.Snapshot(newFormWorkflow()))
		harness.Press("space", "c", "enter")

		assert.NotContains(t, harness.Frame(), "enter submit", "no action ran on the row the cursor landed on")
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceUpdateAgentProcedure))
		assert.Contains(t, readOpenMessages(harness), `"author" is gone`)

		harness.Press("esc", "space", "c")
		assert.Contains(t, harness.Frame(), "enter submit", "the keys are the list's again")
	})

	t.Run("a listing landing under the field leaves it open", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"))...)

		harness.Watch(paneltest.Snapshot(newFormWorkflow(), newFormAuthor(), newAgent("a2", "another", "w1")))

		assert.Equal(t, "└─ author-v2▏", paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("the field takes no keys while the name is being saved", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Hold(agentv1connect.AgentServiceUpdateAgentProcedure)

		harness.Press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"enter", "x", "k"})...)
		assert.Equal(t, "└─ author-v2", paneltest.HoveredRow(harness.Frame()), "the typed name stands without a cursor")

		harness.Release(agentv1connect.AgentServiceUpdateAgentProcedure)
		harness.Press("k")
		assert.Equal(t, "api", paneltest.HoveredRow(harness.Frame()), "and the keys go back to the list once it lands")
	})

	t.Run("a saved name stands until the listing shows it", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.Press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"enter"})...)
		assert.Equal(t, "└─ author-v2", paneltest.HoveredRow(harness.Frame()), "not the old name before the watch catches up")

		harness.Watch(paneltest.Snapshot(newFormWorkflow(), newAgent("a1", "author-v2", "w1")))
		harness.Watch(paneltest.Snapshot(newFormWorkflow(), newAgent("a1", "author-v3", "w1")))
		assert.Equal(t, "└─ author-v3", paneltest.HoveredRow(harness.Frame()), "the listing renders the name once it has caught up")
	})

	t.Run("a refused name opens the field again holding it", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.Refuse(agentv1connect.AgentServiceUpdateAgentProcedure, connect.NewError(connect.CodeAlreadyExists, errors.New("name taken")))

		harness.Press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"enter"})...)

		frame := harness.Frame()
		assert.Contains(t, frame, "Could not rename agent")
		require.Equal(t, "└─ author-v2▏", paneltest.HoveredRow(frame))

		harness.Press("!")
		assert.Equal(t, "└─ author-v2!▏", paneltest.HoveredRow(harness.Frame()))
	})
}
