package panel

import (
	"testing"

	"claude-agents/internal/store"
)

func ptr(s string) *string { return &s }

// TestChatRowsGrouping checks chats nest under their workflow header, empty
// workflows still show a header, and unassigned chats trail under a no-workflow
// header.
func TestChatRowsGrouping(t *testing.T) {
	m := model{
		workflows: []store.Workflow{{ID: "w1", Name: "backend"}, {ID: "w2", Name: "frontend"}},
		chats: []chatItem{
			{agent: store.Agent{Name: "auth", WorkflowID: ptr("w1")}},
			{agent: store.Agent{Name: "loose"}},
		},
	}

	rows := m.chatRows()
	if len(rows) != 5 {
		t.Fatalf("want 5 rows, got %d: %+v", len(rows), rows)
	}
	if !rows[0].header || rows[0].label != "backend" || rows[0].workflowIndex != 0 {
		t.Fatalf("row 0: want backend header at index 0, got %+v", rows[0])
	}
	if rows[1].header || rows[1].item.agent.Name != "auth" {
		t.Fatalf("row 1: want chat auth, got %+v", rows[1])
	}
	if !rows[2].header || rows[2].label != "frontend" || rows[2].workflowIndex != 1 {
		t.Fatalf("row 2: want empty frontend header at index 1, got %+v", rows[2])
	}
	if !rows[3].header || rows[3].label != noWorkflowLabel || rows[3].workflowIndex != -1 {
		t.Fatalf("row 3: want no-workflow header, got %+v", rows[3])
	}
	if rows[4].header || rows[4].item.agent.Name != "loose" {
		t.Fatalf("row 4: want chat loose, got %+v", rows[4])
	}
}

// TestHoveredWorkflow checks the cursor reports a real workflow only when it is
// on that workflow's header, not on a chat or the no-workflow header.
func TestHoveredWorkflow(t *testing.T) {
	m := model{
		workflows: []store.Workflow{{ID: "w1", Name: "backend"}},
		chats: []chatItem{
			{agent: store.Agent{Name: "auth", WorkflowID: ptr("w1")}},
			{agent: store.Agent{Name: "loose"}},
		},
	}

	// rows: 0 backend header, 1 auth, 2 (no workflow) header, 3 loose.
	cases := []struct {
		cursor  int
		wantIdx int
		wantOK  bool
	}{
		{0, 0, true},
		{1, 0, false},
		{2, 0, false},
		{3, 0, false},
	}
	for _, c := range cases {
		m.chatCursor = c.cursor
		idx, ok := m.hoveredWorkflow()
		if ok != c.wantOK || (ok && idx != c.wantIdx) {
			t.Fatalf("cursor %d: want (%d,%v), got (%d,%v)", c.cursor, c.wantIdx, c.wantOK, idx, ok)
		}
	}
}
