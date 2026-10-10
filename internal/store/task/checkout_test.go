package task_test

import (
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"komrad/internal/store"
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
				require.NoError(t, stores.Tasks.SetHead(t.Context(), taskID, taskstore.SetHeadParams{WorktreeRemoved: true}))
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

// TestCheckoutRoundTrips pins a checkout claimed and given its directory reading back as written.
func TestCheckoutRoundTrips(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
	task := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})
	assert.Empty(t, task.Checkout.Cwd, "a fresh task has no checkout")

	claimed, err := stores.Tasks.ClaimCheckout(ctx, taskstore.ClaimCheckoutParams{
		ID:            task.ID,
		AdoptedBranch: "feature",
		RepositoryURL: "https://github.com/harrisoncramer/komrad",
	})
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, stores.Tasks.SetCheckoutDir(ctx, task.ID, "/tmp/worktrees/work"))
	require.NoError(t, stores.Tasks.SetDiffStat(ctx, task.ID, &taskstore.DiffStat{Added: 12, Removed: 3}))

	got, err := stores.Tasks.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktrees/work", got.Checkout.Cwd)
	assert.Equal(t, "feature", got.Checkout.AdoptedBranch)
	assert.Equal(t, xptr.Ptr("feature"), got.Checkout.Branch)
	assert.Equal(t, xptr.Ptr("https://github.com/harrisoncramer/komrad"), got.Checkout.RepositoryURL)
	assert.Equal(t, &taskstore.DiffStat{Added: 12, Removed: 3}, got.Checkout.DiffStat)

	require.NoError(t, stores.Tasks.SetDiffStat(ctx, task.ID, nil))
	cleared, err := stores.Tasks.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Nil(t, cleared.Checkout.DiffStat)
}

// TestLastRebaseRoundTrips pins a task's last rebase reading back as written, nil before its first.
func TestLastRebaseRoundTrips(t *testing.T) {
	stores := storetest.Open(t)
	ctx := t.Context()
	workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
	task := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})

	never, err := stores.Tasks.GetLastRebase(ctx, task.ID)
	require.NoError(t, err)
	assert.Nil(t, never)

	require.NoError(t, stores.Tasks.SetLastRebase(ctx, task.ID, json.RawMessage(`{"failure": "first"}`)))
	require.NoError(t, stores.Tasks.SetLastRebase(ctx, task.ID, json.RawMessage(`{"failure": "second"}`)))
	last, err := stores.Tasks.GetLastRebase(ctx, task.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"failure": "second"}`, string(last))

	_, err = stores.Tasks.GetLastRebase(ctx, "01a11e6f-af0b-7497-91fb-d3750f8cbef2")
	require.ErrorIs(t, err, store.ErrNotFound)
}
