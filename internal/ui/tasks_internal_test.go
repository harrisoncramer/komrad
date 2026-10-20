package ui

import (
	"testing"

	"komrad/internal/ui/panel"

	"github.com/stretchr/testify/assert"
)

// TestTaskFieldsDirectory pins the directory a task's record shows, and why it shows none.
func TestTaskFieldsDirectory(t *testing.T) {
	tests := []struct {
		name   string
		reason panel.WorktreeRemovalReason
		want   string
	}{
		{
			name: "a worktree on disk shows its directory",
			want: "/trees/api",
		},
		{
			name:   "a worktree a delete removed says it was deleted",
			reason: panel.WorktreeDeleted,
			want:   "— (deleted)",
		},
		{
			name:   "a worktree gone without a delete says it is gone",
			reason: panel.WorktreeGone,
			want:   "— (gone)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := panel.Task{Cwd: "/trees/api", WorktreeRemovalReason: tt.reason}

			fields := taskFields(checkoutFieldsParams{task: task}, nil)

			assert.Equal(t, recordField{label: "directory", value: tt.want}, fields[0])
		})
	}
}
