package ui_test

import (
	"testing"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"

	"github.com/stretchr/testify/assert"
)

// TestWorktreesJump covers jumping from a directory to the task it belongs to.
func TestWorktreesJump(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		row         string
		wantScreen  screen.ID
		wantHovered string
		wantTaskID  string
	}{
		{
			name:        "a directory nothing answers for stays put",
			row:         "task/orphan",
			wantScreen:  screen.Worktrees,
			wantHovered: "task/orphan",
		},
		{
			name:        "a live task's directory lands on the agents list",
			row:         "treeview",
			wantScreen:  screen.Agents,
			wantHovered: "treeview",
			wantTaskID:  "live",
		},
		{
			name:        "an archived task's directory lands in the archive",
			row:         "reviewer",
			wantScreen:  screen.Agents,
			wantHovered: "reviewer",
			wantTaskID:  "archived",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := paneltest.OpenAgents(t, buildWorktreeRows()...)
			harness.AnswerWorktreeReads()
			harness.Press("shift+tab", "esc")
			harness.MoveToRow(tt.row)

			harness.Press("space", "J", "t")

			frame := harness.Frame()
			assert.Equal(t, tt.wantScreen, paneltest.Screen(frame))
			assert.Contains(t, paneltest.HoveredRow(frame), tt.wantHovered)
			assert.Equal(t, tt.wantTaskID, paneltest.RecordField(frame, "task_id"))
		})
	}
}
