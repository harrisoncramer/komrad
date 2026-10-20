package task_test

import (
	"slices"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
)

// TestClaimCheckout pins that a task's checkout is claimed once, whoever asks.
func TestClaimCheckout(t *testing.T) {
	tests := []struct {
		name    string
		seed    func(t *testing.T, stores *storetest.Stores, taskID string)
		missing bool
		wantErr error
		want    bool
	}{
		{
			name:    "a task that is not an id is not found",
			seed:    func(*testing.T, *storetest.Stores, string) {},
			missing: true,
			wantErr: store.ErrNotFound,
		},
		{
			name: "a task already claimed is not claimed again",
			seed: func(t *testing.T, stores *storetest.Stores, taskID string) {
				claimed, err := stores.Tasks.ClaimCheckout(t.Context(), taskstore.ClaimCheckoutParams{ID: taskID})
				require.NoError(t, err)
				require.True(t, claimed)
			},
			want: false,
		},
		{
			name: "a task whose checkout was taken away is not claimed again",
			seed: func(t *testing.T, stores *storetest.Stores, taskID string) {
				require.NoError(t, stores.Tasks.SetHead(t.Context(), taskID, taskstore.SetHeadParams{WorktreeRemovalReason: taskstore.WorktreeGone}))
			},
			want: false,
		},
		{
			name: "a task with no checkout is claimed",
			seed: func(*testing.T, *storetest.Stores, string) {},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
			task := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})
			tt.seed(t, stores, task.ID)

			id := task.ID
			if tt.missing {
				id = "work"
			}

			got, err := stores.Tasks.ClaimCheckout(t.Context(), taskstore.ClaimCheckoutParams{ID: id})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

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

// TestListCheckouts pins which live checkouts the sync reads, a worktree gone without a delete among
// them so it is cleared once the directory comes back.
func TestListCheckouts(t *testing.T) {
	tests := []struct {
		name   string
		reason taskstore.WorktreeRemovalReason
		want   bool
	}{
		{
			name: "a checkout on disk is listed",
			want: true,
		},
		{
			name:   "a worktree gone without a delete is listed",
			reason: taskstore.WorktreeGone,
			want:   true,
		},
		{
			name:   "a worktree a delete removed is left out",
			reason: taskstore.WorktreeDeleted,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			ctx := t.Context()
			agent := stores.SeedAgent(t, agentstore.Agent{Name: "writer", Role: agentstore.RoleWriter})
			require.NoError(t, stores.Tasks.SetCheckoutDir(ctx, *agent.TaskID, "/tmp/worktrees/work"))
			stores.SeedTaskHead(t, agent.ID, taskstore.SetHeadParams{WorktreeRemovalReason: tt.reason})

			listed, err := stores.Tasks.ListCheckouts(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.want, slices.ContainsFunc(listed, func(task taskstore.Task) bool { return task.ID == *agent.TaskID }))
		})
	}
}
