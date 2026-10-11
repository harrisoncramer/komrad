package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"komrad/internal/gittest"
	"komrad/internal/scriptrun"
	"komrad/internal/store"
	"komrad/internal/storetest"
	"komrad/lib/clock"
	"komrad/lib/xslice"

	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScripts runs no hook and records each task checkout it was asked to give back.
type fakeScripts struct {
	scriptrun.HookQueue
	hold           chan struct{}
	deprovisionErr error
	mu             sync.Mutex
	deprovisioned  []string
}

func (f *fakeScripts) RunHook(context.Context, agentstore.Agent, string, string) error {
	return nil
}

func (f *fakeScripts) RunTaskDeprovision(_ context.Context, task taskstore.Task, _ string) error {
	f.mu.Lock()
	f.deprovisioned = append(f.deprovisioned, task.Checkout.Cwd)
	f.mu.Unlock()

	if f.hold != nil {
		<-f.hold
	}

	return f.deprovisionErr
}

func (f *fakeScripts) ranDeprovisions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.deprovisioned)
}

type deleteFixture struct {
	stores    *storetest.Stores
	svc       *TaskService
	workflow  *workflowstore.Workflow
	worktrees *worktreestore.WorktreeStore
	reviews   *reviewstore.ReviewStore
	snapshots *gittest.Snapshots
}

func newDeleteFixture(t *testing.T, scripts *fakeScripts) *deleteFixture {
	t.Helper()

	stores := storetest.Open(t)
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	workflow := stores.SeedWorkflow(t, workflowstore.Config{
		Name:     "backend",
		Cwd:      repo,
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	worktrees := worktreestore.NewWorktreeStore(stores.Pool.GetDB())
	reviews := reviewstore.NewReviewStore(stores.Pool.GetDB())
	snapshots := &gittest.Snapshots{}

	return &deleteFixture{
		stores:    stores,
		workflow:  workflow,
		worktrees: worktrees,
		reviews:   reviews,
		snapshots: snapshots,
		svc: NewTaskService(NewTaskServiceParams{
			Tasks:     stores.Tasks,
			Agents:    stores.Agents,
			Workflows: stores.Workflows,
			Worktrees: worktrees,
			Reviews:   reviews,
			Snapshots: snapshots,
			Scripts:   scripts,
			Tx:        stores.Pool,
		}),
	}
}

// TestDeleteIfEmptyGivesCheckoutBackOnce pins that cleanups finishing together give a task's checkout back once.
func TestDeleteIfEmptyGivesCheckoutBackOnce(t *testing.T) {
	scripts := &fakeScripts{hold: make(chan struct{})}
	fx := newDeleteFixture(t, scripts)
	task := fx.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: fx.workflow.ID, Slug: "work"})
	require.NoError(t, fx.stores.Tasks.SetCheckoutDir(t.Context(), task.ID, t.TempDir()))
	require.NoError(t, fx.stores.Tasks.MarkDeleting(t.Context(), []string{task.ID}))

	const cleanups = 8
	var wg sync.WaitGroup
	errs := make([]error, cleanups)
	for i := range cleanups {
		wg.Go(func() {
			errs[i] = fx.svc.DeleteIfEmpty(t.Context(), task.ID)
		})
	}

	require.Eventually(t, func() bool { return len(scripts.ranDeprovisions()) == 1 }, 5*time.Second, 10*time.Millisecond)
	clock.Sleep(t.Context(), 100*time.Millisecond)
	close(scripts.hold)
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Len(t, scripts.ranDeprovisions(), 1)
	_, err := fx.stores.Tasks.Get(t.Context(), task.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestDeleteIfEmptyForgetsItsWorktree covers a deleted task's worktree row going with it, and
// a checkout that could not be given back keeping both.
func TestDeleteIfEmptyForgetsItsWorktree(t *testing.T) {
	tests := []struct {
		name           string
		deprovisionErr error
		wantErr        bool
		wantDirs       func(dir, other string) []string
	}{
		{
			name:           "a checkout that could not be given back keeps the task and its row",
			deprovisionErr: errors.New("the hook failed"),
			wantErr:        true,
			wantDirs: func(dir, other string) []string {
				return []string{dir, other}
			},
		},
		{
			name: "a deleted task's row is forgotten and another task's kept",
			wantDirs: func(_, other string) []string {
				return []string{other}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newDeleteFixture(t, &fakeScripts{deprovisionErr: tt.deprovisionErr})
			task := fx.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: fx.workflow.ID, Slug: "work"})
			dir := filepath.Join(t.TempDir(), task.ID)
			other := filepath.Join(filepath.Dir(dir), fx.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: fx.workflow.ID, Slug: "other"}).ID)
			require.NoError(t, fx.stores.Tasks.SetCheckoutDir(t.Context(), task.ID, dir))
			for _, tree := range []string{dir, other} {
				require.NoError(t, fx.worktrees.Upsert(t.Context(), worktreestore.Worktree{Dir: tree}))
			}
			require.NoError(t, fx.stores.Tasks.MarkDeleting(t.Context(), []string{task.ID}))

			err := fx.svc.DeleteIfEmpty(t.Context(), task.ID)

			if tt.wantErr {
				require.Error(t, err)
				_, err := fx.stores.Tasks.Get(t.Context(), task.ID)
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
			}
			listed, err := fx.worktrees.List(t.Context())
			require.NoError(t, err)
			assert.ElementsMatch(t, tt.wantDirs(dir, other), xslice.Map(listed, func(worktree worktreestore.Worktree) string { return worktree.Dir }))
		})
	}
}

// TestDeleteIfEmptyReleasesItsRounds pins that a deleted task lets go of the refs holding its own
// rounds' snapshots, from a checkout that still stands, and only once its checkout was given back.
func TestDeleteIfEmptyReleasesItsRounds(t *testing.T) {
	tests := []struct {
		name            string
		deprovisionErr  error
		checkoutGone    bool
		wantWorkflowDir bool
		wantReleased    bool
	}{
		{
			name:         "a checkout that stands",
			wantReleased: true,
		},
		{
			name:            "a checkout already removed hands the release to the workflow's directory",
			checkoutGone:    true,
			wantWorkflowDir: true,
			wantReleased:    true,
		},
		{
			name:           "a checkout that could not be given back keeps them",
			deprovisionErr: errors.New("the hook failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newDeleteFixture(t, &fakeScripts{deprovisionErr: tt.deprovisionErr})
			task := fx.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: fx.workflow.ID, Slug: "work"})
			other := fx.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: fx.workflow.ID, Slug: "other"})
			dir := t.TempDir()
			require.NoError(t, fx.stores.Tasks.SetCheckoutDir(t.Context(), task.ID, dir))
			for _, id := range []string{task.ID, other.ID} {
				_, err := fx.reviews.CreateRound(t.Context(), reviewstore.CreateRoundParams{
					AgentID:     uuid.NewString(),
					TaskID:      id,
					BaseSHA:     "base",
					SnapshotSHA: "same-sha",
				})
				require.NoError(t, err)
			}
			if tt.checkoutGone {
				require.NoError(t, os.RemoveAll(dir))
			}
			require.NoError(t, fx.stores.Tasks.MarkDeleting(t.Context(), []string{task.ID}))

			err := fx.svc.DeleteIfEmpty(t.Context(), task.ID)

			if !tt.wantReleased {
				require.Error(t, err)
				assert.Empty(t, fx.snapshots.Released)

				return
			}
			require.NoError(t, err)
			want := dir
			if tt.wantWorkflowDir {
				want = fx.workflow.Cwd
			}
			assert.Equal(t, []string{reviewstore.RoundRef(task.ID, "same-sha")}, fx.snapshots.Released,
				"only the deleted task's ref goes, though the other task's names the same snapshot")
			assert.Equal(t, []string{want}, fx.snapshots.ReleasedFrom)
		})
	}
}
