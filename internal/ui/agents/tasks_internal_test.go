package agents

import (
	"testing"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/stretchr/testify/assert"
)

// TestTaskFieldsDirectory pins the directory a task's record shows, and why it shows none.
func TestTaskFieldsDirectory(t *testing.T) {
	tests := []struct {
		name   string
		reason taskv1.WorktreeRemovalReason
		want   string
	}{
		{
			name: "a worktree on disk shows its directory",
			want: "/trees/api",
		},
		{
			name:   "a worktree a delete removed says it was deleted",
			reason: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED,
			want:   "— (deleted)",
		},
		{
			name:   "a worktree gone without a delete says it is gone",
			reason: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE,
			want:   "— (gone)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := panel.Task{Cwd: "/trees/api", WorktreeRemovalReason: tt.reason}

			fields := taskFields(checkoutFieldsParams{task: task}, nil)

			assert.Equal(t, record.Field{Label: "directory", Value: tt.want}, fields[0])
		})
	}
}
