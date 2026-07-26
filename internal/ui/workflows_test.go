package ui

import (
	"testing"

	"claude-agents/internal/store/session"
	workflowstore "claude-agents/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChatRows checks the tree the list renders: every workflow is a header with
// its chats beneath it, and chats belonging to none trail under a header of
// their own.
func TestChatRows(t *testing.T) {
	tests := []struct {
		name      string
		workflows []workflowstore.Workflow
		chats     []chatItem
		wantRows  []string
	}{
		{
			name:     "an empty store still shows the no-workflow header",
			wantRows: []string{"header:" + noWorkflowLabel},
		},
		{
			name:      "an empty workflow keeps its header, so a chat can be added to it",
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			wantRows: []string{
				"header:backend",
				"header:" + noWorkflowLabel,
			},
		},
		{
			name: "chats nest under their workflow and unassigned ones trail",
			workflows: []workflowstore.Workflow{
				{ID: "w1", Name: "backend"},
				{ID: "w2", Name: "frontend"},
			},
			chats: []chatItem{
				{agent: session.Agent{Name: "auth", WorkflowID: ptr("w1")}},
				{agent: session.Agent{Name: "loose"}},
			},
			wantRows: []string{
				"header:backend",
				"chat:auth",
				"header:frontend",
				"header:" + noWorkflowLabel,
				"chat:loose",
			},
		},
		{
			name: "a chat whose workflow is gone falls in with the unassigned",
			chats: []chatItem{
				{agent: session.Agent{Name: "orphan", WorkflowID: ptr("vanished")}},
			},
			wantRows: []string{
				"header:" + noWorkflowLabel,
				"chat:orphan",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{workflows: tt.workflows, chats: tt.chats}

			assert.Equal(t, tt.wantRows, describeRows(m.chatRows()))
		})
	}
}

// TestHoveredWorkflow checks the cursor reports a real workflow only when it
// sits on that workflow's header, since the workflow keys act on what is hovered.
func TestHoveredWorkflow(t *testing.T) {
	tests := []struct {
		name    string
		cursor  int
		wantIdx int
		wantOK  bool
	}{
		{
			name:   "on a chat",
			cursor: 1,
		},
		{
			name:   "on the no-workflow header, which has no record behind it",
			cursor: 2,
		},
		{
			name:   "on a chat belonging to no workflow",
			cursor: 3,
		},
		{
			name:    "on a workflow header",
			cursor:  0,
			wantIdx: 0,
			wantOK:  true,
		},
	}

	// rows: 0 backend header, 1 auth, 2 no-workflow header, 3 loose.
	m := model{
		workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
		chats: []chatItem{
			{agent: session.Agent{Name: "auth", WorkflowID: ptr("w1")}},
			{agent: session.Agent{Name: "loose"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.chatCursor = tt.cursor

			idx, ok := m.hoveredWorkflow()
			require.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantIdx, idx)
			}
		})
	}
}

// describeRows flattens the tree into strings, so a case states the shape it
// expects rather than asserting field by field.
func describeRows(rows []chatRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.header {
			out = append(out, "header:"+row.label)
			continue
		}

		out = append(out, "chat:"+row.item.agent.Name)
	}

	return out
}

func ptr(s string) *string { return &s }
