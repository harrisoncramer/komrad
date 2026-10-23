package task_test

import (
	"slices"
	"sync/atomic"
	"testing"

	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// TestClaimCheckout pins that a task's checkout is claimed once, whoever asks.
func TestClaimCheckout(t *testing.T) {
	tests := []struct {
		name string
		seed func(t *testing.T, stores *storetest.Stores, taskID uuid.UUID)
		want bool
	}{
		{
			name: "a task already claimed is not claimed again",
			seed: func(t *testing.T, stores *storetest.Stores, taskID uuid.UUID) {
				claimed, err := stores.Tasks.ClaimCheckout(t.Context(), taskstore.ClaimCheckoutParams{ID: taskID})
				require.NoError(t, err)
				require.True(t, claimed)
			},
			want: false,
		},
		{
			name: "a task whose checkout was taken away is not claimed again",
			seed: func(t *testing.T, stores *storetest.Stores, taskID uuid.UUID) {
				require.NoError(t, stores.Tasks.SetHead(t.Context(), taskID, taskstore.SetHeadParams{WorktreeRemovalReason: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE}))
			},
			want: false,
		},
		{
			name: "a task with no checkout is claimed",
			seed: func(*testing.T, *storetest.Stores, uuid.UUID) {},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
			task := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})
			tt.seed(t, stores, task.ID)

			got, err := stores.Tasks.ClaimCheckout(t.Context(), taskstore.ClaimCheckoutParams{ID: task.ID})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestClaimCheckoutConcurrently pins that launches claiming one task at once leave a single winner.
func TestClaimCheckoutConcurrently(t *testing.T) {
	stores := storetest.Open(t)
	workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
	task := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})

	var claims atomic.Int32
	group, ctx := errgroup.WithContext(t.Context())
	for range 8 {
		group.Go(func() error {
			claimed, err := stores.Tasks.ClaimCheckout(ctx, taskstore.ClaimCheckoutParams{ID: task.ID})
			if claimed {
				claims.Add(1)
			}

			return err
		})
	}
	require.NoError(t, group.Wait())

	assert.Equal(t, int32(1), claims.Load())
}

// TestClaimCheckoutAgain covers a retried launch taking up the claim a failed one gave back, on the
// terms the failed one made it.
func TestClaimCheckoutAgain(t *testing.T) {
	tests := []struct {
		name    string
		release bool
		want    bool
	}{
		{
			name: "a task still claimed is not claimed again",
			want: false,
		},
		{
			name:    "a task given back is claimed again on the branch it was put on",
			release: true,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			ctx := t.Context()
			workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
			task := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})

			claimed, err := stores.Tasks.ClaimCheckout(ctx, taskstore.ClaimCheckoutParams{ID: task.ID, AdoptedBranch: "feature"})
			require.NoError(t, err)
			require.True(t, claimed)
			require.NoError(t, stores.Tasks.SetCheckoutDir(ctx, task.ID, "/tmp/worktrees/work"))
			if tt.release {
				require.NoError(t, stores.Tasks.ReleaseCheckout(ctx, task.ID))
			}

			got, err := stores.Tasks.ClaimCheckoutAgain(ctx, task.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			if tt.release {
				reclaimed, err := stores.Tasks.Get(ctx, task.ID)
				require.NoError(t, err)
				assert.Empty(t, reclaimed.Checkout.Cwd, "the directory the failed launch made is gone")
				assert.Equal(t, xptr.Ptr("feature"), reclaimed.Checkout.Branch)
			}
		})
	}
}

// TestListCheckouts pins which live checkouts the cwd sync reads, leaving out a deleted worktree.
func TestListCheckouts(t *testing.T) {
	tests := []struct {
		name   string
		reason taskv1.WorktreeRemovalReason
		want   bool
	}{
		{
			name: "a checkout on disk is listed",
			want: true,
		},
		{
			name:   "a worktree gone without a delete is listed",
			reason: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE,
			want:   true,
		},
		{
			name:   "a worktree a delete removed is left out",
			reason: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			ctx := t.Context()
			agent := stores.SeedAgent(t, agentstore.Agent{Name: "writer", Role: agentv1.AgentRole_AGENT_ROLE_WRITER})
			require.NoError(t, stores.Tasks.SetCheckoutDir(ctx, *agent.TaskID, "/tmp/worktrees/work"))
			stores.SeedTaskHead(t, agent.ID, taskstore.SetHeadParams{WorktreeRemovalReason: tt.reason})

			listed, err := stores.Tasks.ListCheckouts(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, slices.ContainsFunc(listed, func(task taskstore.Task) bool { return task.ID == *agent.TaskID }))
		})
	}
}

// TestSetHead pins the removal reason a head write leaves, which never replaces a deleted one.
func TestSetHead(t *testing.T) {
	tests := []struct {
		name  string
		from  taskv1.WorktreeRemovalReason
		write taskv1.WorktreeRemovalReason
		want  taskv1.WorktreeRemovalReason
	}{
		{
			name:  "a worktree found missing is recorded gone",
			write: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE,
			want:  taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE,
		},
		{
			name:  "a gone worktree back on disk is cleared",
			from:  taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE,
			write: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED,
			want:  taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED,
		},
		{
			name:  "a deleted worktree found missing stays deleted",
			from:  taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED,
			write: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE,
			want:  taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED,
		},
		{
			name:  "a deleted worktree read as on disk stays deleted",
			from:  taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED,
			write: taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_UNSPECIFIED,
			want:  taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_DELETED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			ctx := t.Context()
			task := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})
			require.NoError(t, stores.Tasks.SetCheckoutDir(ctx, task.ID, "/tmp/worktrees/work"))
			require.NoError(t, stores.Tasks.SetHead(ctx, task.ID, taskstore.SetHeadParams{WorktreeRemovalReason: tt.from}))

			require.NoError(t, stores.Tasks.SetHead(ctx, task.ID, taskstore.SetHeadParams{WorktreeRemovalReason: tt.write}))

			got, err := stores.Tasks.Get(ctx, task.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Checkout.WorktreeRemovalReason)
		})
	}
}
