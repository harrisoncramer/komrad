package agents

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"komrad/internal/store"
	taskstore "komrad/internal/store/task"
	worktreestore "komrad/internal/store/worktree"
	"komrad/lib/clock"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmptyTaskCleanupRunsOnce pins that cleanups finishing together give a task's checkout back once.
func TestEmptyTaskCleanupRunsOnce(t *testing.T) {
	hold := make(chan struct{})
	lifecycle := &fakeLifecycle{holdTaskDeprovision: hold}
	fx := newTaskDeleteFixture(t, lifecycle, worktreeConfig(t, "backend"))
	task := fx.st.SeedTask(t, taskstore.CreateParams{WorkflowID: fx.workflow.ID, Slug: "work"})
	require.NoError(t, fx.st.Tasks.SetCheckoutDir(t.Context(), task.ID, t.TempDir()))
	require.NoError(t, fx.st.Tasks.MarkDeleting(t.Context(), []string{task.ID}))

	const cleanups = 8
	var wg sync.WaitGroup
	errs := make([]error, cleanups)
	for i := range cleanups {
		wg.Go(func() {
			errs[i] = fx.agents.emptyTasks.DeleteIfEmpty(t.Context(), task.ID)
		})
	}

	require.Eventually(t, func() bool { return len(lifecycle.ranTaskDeprovisions()) == 1 }, 5*time.Second, 10*time.Millisecond)
	clock.Sleep(t.Context(), 100*time.Millisecond)
	close(hold)
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Len(t, lifecycle.ranTaskDeprovisions(), 1)
	_, err := fx.st.Tasks.Get(t.Context(), task.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestEmptyTaskDeleteForgetsItsWorktree covers a deleted task's worktree row going with it, and
// a checkout that could not be given back keeping both.
func TestEmptyTaskDeleteForgetsItsWorktree(t *testing.T) {
	tests := []struct {
		name           string
		deprovisionErr error
		wantErr        bool
		wantDirs       func(dir string) []string
	}{
		{
			name:           "a checkout that could not be given back keeps the task and its row",
			deprovisionErr: errors.New("the hook failed"),
			wantErr:        true,
			wantDirs: func(dir string) []string {
				return []string{dir, "/trees/other"}
			},
		},
		{
			name: "a deleted task's row is forgotten and another task's kept",
			wantDirs: func(string) []string {
				return []string{"/trees/other"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{taskDeprovisionErr: tt.deprovisionErr}
			fx := newTaskDeleteFixture(t, lifecycle, worktreeConfig(t, "backend"))
			worktrees := worktreestore.NewWorktreeStore(fx.st.Pool.GetDB())
			task := fx.st.SeedTask(t, taskstore.CreateParams{WorkflowID: fx.workflow.ID, Slug: "work"})
			dir := filepath.Join(t.TempDir(), task.ID)
			require.NoError(t, fx.st.Tasks.SetCheckoutDir(t.Context(), task.ID, dir))
			for _, tree := range []string{dir, "/trees/other"} {
				require.NoError(t, worktrees.Upsert(t.Context(), worktreestore.Worktree{Dir: tree}))
			}
			require.NoError(t, fx.st.Tasks.MarkDeleting(t.Context(), []string{task.ID}))

			err := fx.agents.emptyTasks.DeleteIfEmpty(t.Context(), task.ID)

			if tt.wantErr {
				require.Error(t, err)
				_, err := fx.st.Tasks.Get(t.Context(), task.ID)
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
			}
			listed, err := worktrees.List(t.Context())
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantDirs(dir), xslice.Map(listed, func(worktree worktreestore.Worktree) string { return worktree.Dir }))
		})
	}
}
