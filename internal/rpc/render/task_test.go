package render

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/testing/protocmp"

	taskstore "komrad/internal/store/task"
	"komrad/lib/xptr"
	taskv1 "komrad/proto/gen/task/v1"
)

// TestTask pins the checkout fields a client cannot draw unless the task carries them.
func TestTask(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, time.September, 1, 9, 9, 0, 0, time.UTC)

	tests := []struct {
		name   string
		stored taskstore.Task
		want   *taskv1.Task
	}{
		{
			name: "a task with no checkout",
			stored: taskstore.Task{
				ID:        "t1",
				Slug:      "fresh",
				CreatedAt: created,
				UpdatedAt: created,
			},
			want: &taskv1.Task{
				Id:        "t1",
				Slug:      "fresh",
				CreatedAt: "2026-09-01T09:09:00Z",
				UpdatedAt: "2026-09-01T09:09:00Z",
			},
		},
		{
			name: "a task the syncers have written to",
			stored: taskstore.Task{
				ID:            "t2",
				Slug:          "worked",
				FailureReason: "teardown exited 1",
				CreatedAt:     created,
				UpdatedAt:     created,
				Checkout: taskstore.Checkout{
					Cwd:                   "/tmp/worktrees/worked",
					Branch:                xptr.Ptr("harry/spike"),
					AdoptedBranch:         "harry/spike",
					WorktreeRemovalReason: taskstore.WorktreeGone,
					RepositoryURL:         xptr.Ptr("https://github.com/harrisoncramer/komrad"),
					HeadSHA:               xptr.Ptr("abc"),
					Sequence:              taskstore.SequenceRebase,
					MovedSinceSnapshot:    "snapshot-one",
					DiffStat:              &taskstore.DiffStat{Added: 12, Removed: 3},
				},
			},
			want: &taskv1.Task{
				Id:                    "t2",
				Slug:                  "worked",
				CreatedAt:             "2026-09-01T09:09:00Z",
				UpdatedAt:             "2026-09-01T09:09:00Z",
				Cwd:                   xptr.Ptr("/tmp/worktrees/worked"),
				Branch:                xptr.Ptr("harry/spike"),
				AdoptedBranch:         xptr.Ptr("harry/spike"),
				WorktreeRemovalReason: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE,
				RepositoryUrl:         xptr.Ptr("https://github.com/harrisoncramer/komrad"),
				HeadSha:               xptr.Ptr("abc"),
				Sequence:              taskv1.Sequence_SEQUENCE_REBASE,
				MovedSinceSnapshot:    "snapshot-one",
				DiffStat:              &taskv1.DiffStat{Added: 12, Removed: 3},
				FailureReason:         "teardown exited 1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Empty(t, cmp.Diff(tt.want, Task(tt.stored), protocmp.Transform()))
		})
	}
}
