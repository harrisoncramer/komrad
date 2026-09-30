package ui_test

import (
	"errors"
	"slices"
	"testing"

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

// TestRenameSends covers a name typed over its row going out as the write the service renames by.
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

			harness.press(tt.press...)

			harness.assertSentTo(tt.procedure, tt.want...)
			assert.NotContains(t, readListRows(harness.readFrame()), "  author-v2▏", "the field has closed")
		})
	}
}

// TestRenameField covers the name being typed over its row, drawn where the name was.
func TestRenameField(t *testing.T) {
	t.Run("the field opens on the name the row had", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press("j", "r")

		frame := harness.readFrame()
		assert.Equal(t, "  author▏", readHoveredRow(frame))
		assert.Contains(t, readLegend(frame), "enter save")
		assert.Contains(t, readLegend(frame), "esc cancel")
		assert.NotContains(t, frame, "C-s submit", "no form opens")
	})

	t.Run("a paste goes in on one line", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press("j", "r")
		harness.send(tea.PasteMsg{Content: "-a\nb"})

		assert.Equal(t, "  author-a b▏", readHoveredRow(harness.readFrame()))
	})

	t.Run("esc puts the name back and the keys go back to the list", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())

		harness.press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"esc", "k"})...)

		assert.Equal(t, "api", readHoveredRow(harness.readFrame()))
		assert.Contains(t, readListRows(harness.readFrame()), "  author")
	})

	t.Run("the field closes when its row leaves the list", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"))...)

		harness.watch(buildSnapshot(newFormWorkflow()))
		harness.press("x")

		assert.NotContains(t, harness.readFrame(), "author-v2")
		assert.Empty(t, harness.readSent(agentv1connect.AgentServiceUpdateAgentProcedure))
	})

	t.Run("a listing landing under the field leaves it open", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"))...)

		harness.watch(buildSnapshot(newFormWorkflow(), newFormAuthor(), newAgent("a2", "another", "w1")))

		assert.Equal(t, "  author-v2▏", readHoveredRow(harness.readFrame()))
	})

	t.Run("the field takes no keys while the name is being saved", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.hold(agentv1connect.AgentServiceUpdateAgentProcedure)

		harness.press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"enter", "x", "k"})...)
		assert.Equal(t, "  author-v2", readHoveredRow(harness.readFrame()), "the typed name stands without a cursor")

		harness.release(agentv1connect.AgentServiceUpdateAgentProcedure)
		harness.press("k")
		assert.Equal(t, "api", readHoveredRow(harness.readFrame()), "and the keys go back to the list once it lands")
	})

	t.Run("a refused name opens the field again holding it", func(t *testing.T) {
		harness := openForms(t, newFormWorkflow(), newFormAuthor())
		harness.refuse(agentv1connect.AgentServiceUpdateAgentProcedure, connect.NewError(connect.CodeAlreadyExists, errors.New("name taken")))

		harness.press(slices.Concat([]string{"j", "r"}, spellKeys("-v2"), []string{"enter"})...)

		frame := harness.readFrame()
		assert.Contains(t, frame, "Could not rename agent")
		require.Equal(t, "  author-v2▏", readHoveredRow(frame))

		harness.press("!")
		assert.Equal(t, "  author-v2!▏", readHoveredRow(harness.readFrame()))
	})
}
