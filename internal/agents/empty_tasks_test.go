package agents

import (
	"sync"
	"testing"
	"time"

	"komrad/internal/store"
	"komrad/lib/clock"

	taskstore "komrad/internal/store/task"

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
