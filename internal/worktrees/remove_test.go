package worktrees

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/git"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveTakesAnUnownedDirectoryAndPrunesIt covers komrad removing a readable worktree no task
// owns and pruning its repository.
func TestRemoveTakesAnUnownedDirectoryAndPrunesIt(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cutUnowned(t)

	removal, err := fixture.newRemover().Remove(t.Context(), []string{dir})
	require.NoError(t, err)

	assert.Equal(t, []string{dir}, removal.Removed)
	assert.Empty(t, removal.Refused)
	assert.NoDirExists(t, dir)
	assert.Equal(t, []string{fixture.repo}, fixture.git.Pruned)
}

// TestRemoveTakesAGoneRegistrationWithoutPruning covers komrad removing a directory git cannot
// read without attempting a prune against a repository that is not there.
func TestRemoveTakesAGoneRegistrationWithoutPruning(t *testing.T) {
	fixture := newFixture(t)
	dir := filepath.Join(fixture.shared, "bbbb")
	write(t, dir, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/bbbb\n")

	removal, err := fixture.newRemover().Remove(t.Context(), []string{dir})
	require.NoError(t, err)

	assert.Equal(t, []string{dir}, removal.Removed)
	assert.NoDirExists(t, dir)
	assert.Empty(t, fixture.git.Pruned)
}

// TestRemoveRefuses covers each directory the remover leaves alone and whether a step broke to leave it.
func TestRemoveRefuses(t *testing.T) {
	tests := []struct {
		name       string
		lay        func(t *testing.T, fixture *worktreeFixture) string
		wantReason string
		wantFailed bool
	}{
		{
			name: "a directory whose task is being deleted",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
				require.NoError(t, fixture.stores.Tasks.MarkDeleting(t.Context(), []string{filepath.Base(dir)}))

				return dir
			},
			wantReason: "is being deleted",
		},
		{
			name: "an archived task's directory",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
				fixture.archiveTask(t, dir)

				return dir
			},
			wantReason: "delete the task instead",
		},
		{
			name: "a live task's directory with an agent in it",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
				fixture.own(t, dir, agentstore.Agent{Name: "live-agent"})

				return dir
			},
			wantReason: "delete the task instead",
		},
		{
			name: "a live task's directory no agent is left in",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				return fixture.cut(t)
			},
			wantReason: "delete the task instead",
		},
		{
			name: "a directory another agent works in",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cutUnowned(t)
				fixture.occupy(t, dir, "reviewer")

				return dir
			},
			wantReason: "reviewer",
		},
		{
			name: "a directory an agent works somewhere under",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cutUnowned(t)
				fixture.occupy(t, filepath.Join(dir, "packages", "api"), "under-agent")

				return dir
			},
			wantReason: "under-agent",
		},
		{
			name: "a path outside the worktree directories",
			lay: func(t *testing.T, _ *worktreeFixture) string {
				return t.TempDir()
			},
			wantReason: "not under the worktree directories",
		},
		{
			name: "a path that is not there, which could not be looked up",
			lay: func(_ *testing.T, fixture *worktreeFixture) string {
				return filepath.Join(fixture.shared, "missing")
			},
			wantReason: "failed to resolve",
			wantFailed: true,
		},
		{
			name: "a directory whose repository's registrations could not be read",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				fixture.git.ListErr = errors.New("git is not on the path")

				return fixture.cutUnowned(t)
			},
			wantReason: "could not list the worktrees",
			wantFailed: true,
		},
		{
			name: "a directory that could not be removed",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cutUnowned(t)
				require.NoError(t, os.Chmod(fixture.shared, 0o500))
				t.Cleanup(func() {
					assert.NoError(t, os.Chmod(fixture.shared, 0o700))
				})

				return dir
			},
			wantReason: "could not remove it",
			wantFailed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newFixture(t)
			dir := tt.lay(t, fixture)

			_, statErr := os.Stat(dir)
			existed := statErr == nil

			removal, err := fixture.newRemover().Remove(t.Context(), []string{dir})
			require.NoError(t, err)

			assert.Empty(t, removal.Removed)
			require.Len(t, removal.Refused, 1)
			assert.Contains(t, removal.Refused[0].Reason, tt.wantReason)
			assert.Equal(t, tt.wantFailed, removal.Refused[0].Failed)
			assert.Empty(t, fixture.git.Pruned)
			if existed {
				assert.DirExists(t, dir)
			}
		})
	}
}

// TestRemoveRefusesADirectoryHoldingAnotherWorktree covers a directory being left alone while
// a worktree sits inside it, whether the listing found that one under a workflow's directory
// or only the repository has it registered.
func TestRemoveRefusesADirectoryHoldingAnotherWorktree(t *testing.T) {
	fixture := newFixture(t)
	listed := fixture.cutUnowned(t)
	fixture.stores.SeedWorkflow(t, workflowstore.Config{Name: "nested", Worktree: workflowstore.WorktreeConfig{Dir: listed}})
	inner := filepath.Join(listed, "inner")
	write(t, inner, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/inner\n")
	registered := fixture.cutUnowned(t)
	byHand := filepath.Join(registered, "by-hand")
	write(t, byHand, ".git", "gitdir: "+filepath.Join(fixture.repo, ".git", "worktrees", "by-hand")+"\n")
	fixture.git.Registered = []git.Worktree{{Dir: fixture.repo}, {Dir: byHand}}

	removal, err := fixture.newRemover().Remove(t.Context(), []string{listed, registered})
	require.NoError(t, err)

	assert.Empty(t, removal.Removed)
	require.Len(t, removal.Refused, 2)
	assert.Contains(t, removal.Refused[0].Reason, inner)
	assert.Contains(t, removal.Refused[1].Reason, byHand)
	assert.False(t, removal.Refused[0].Failed, "a verdict about the directory, not a step that broke")
	assert.False(t, removal.Refused[1].Failed)
	assert.DirExists(t, inner)
	assert.DirExists(t, byHand)
}
