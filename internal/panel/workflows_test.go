package panel

import (
	"testing"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

// TestMakeRowsScope checks the workflows row shows only at the root, and the
// scoped view drops it so it does not nest another workflow list.
func TestMakeRowsScope(t *testing.T) {
	tests := []struct {
		name       string
		workflow   *store.Workflow
		wantKinds  []rowKind
		wantChatsN string
	}{
		{
			name:       "root shows the workflows row",
			workflow:   nil,
			wantKinds:  []rowKind{rowNewChat, rowChats, rowWorkflows, rowSearch},
			wantChatsN: "▸  chats (2)",
		},
		{
			name:       "scoped hides the workflows row",
			workflow:   &store.Workflow{ID: "w1", Name: "backend"},
			wantKinds:  []rowKind{rowNewChat, rowChats, rowSearch},
			wantChatsN: "▸  chats (2)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				workflow:  tt.workflow,
				chats:     items("a", "b"),
				workflows: []store.Workflow{{ID: "w1", Name: "backend"}, {ID: "w2", Name: "frontend"}},
			}

			rows := m.makeRows()
			if len(rows) != len(tt.wantKinds) {
				t.Fatalf("want %d rows, got %d", len(tt.wantKinds), len(rows))
			}
			for i, want := range tt.wantKinds {
				if rows[i].kind != want {
					t.Fatalf("row %d: want kind %d, got %d", i, want, rows[i].kind)
				}
			}
			if rows[1].label != tt.wantChatsN {
				t.Fatalf("chats label: want %q, got %q", tt.wantChatsN, rows[1].label)
			}
		})
	}
}

// TestWorkflowRowsMoveMode confirms the picker gains a leading unassign row when
// reassigning a chat, and shows only the workflow names when browsing.
func TestWorkflowRowsMoveMode(t *testing.T) {
	workflows := []store.Workflow{{ID: "w1", Name: "backend"}, {ID: "w2", Name: "frontend"}}

	browse := model{workflows: workflows}
	if got := browse.workflowRows(); len(got) != 2 || got[0] != "backend" || got[1] != "frontend" {
		t.Fatalf("browse rows: got %v", got)
	}

	move := model{workflows: workflows, moveTarget: &store.Agent{Name: "chat"}}
	got := move.workflowRows()
	if len(got) != 3 || got[0] != unassignLabel || got[1] != "backend" {
		t.Fatalf("move rows: got %v", got)
	}
}

// TestWorkflowCursorClampsToMoveRows drives the picker's down key in move mode,
// where the list length includes the leading unassign row, and confirms the
// cursor stops at the last row rather than running past it.
func TestWorkflowCursorClampsToMoveRows(t *testing.T) {
	m := model{
		screen:     screenWorkflows,
		keys:       appconfig.DefaultKeybindings(),
		workflows:  []store.Workflow{{ID: "w1", Name: "backend"}, {ID: "w2", Name: "frontend"}},
		moveTarget: &store.Agent{Name: "chat"},
	}

	down := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}
	var current tea.Model = m
	for range 5 {
		next, _ := current.(model).updateWorkflows(down)
		current = next
	}

	// Three rows in move mode (unassign + two workflows), so the last index is 2.
	if got := current.(model).workflowCursor; got != 2 {
		t.Fatalf("want cursor clamped to 2, got %d", got)
	}
}
