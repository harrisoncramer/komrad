package worktrees

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveTakesAnUnownedDirectoryAndPrunesIt covers a readable worktree nothing answers
// for going and its repository being pruned.
func TestRemoveTakesAnUnownedDirectoryAndPrunesIt(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)

	removal, err := fixture.newRemover().Remove(t.Context(), []string{dir})
	require.NoError(t, err)

	assert.Equal(t, []string{dir}, removal.Removed)
	assert.Empty(t, removal.Refused)
	assert.NoDirExists(t, dir)
	assert.Equal(t, []string{fixture.repo}, fixture.git.Pruned)
}

// TestRemoveTakesAGoneRegistrationWithoutPruning covers a directory git cannot read going
// without a prune being attempted against a repository that is not there.
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
			name: "a live agent's directory",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
				fixture.own(t, dir, agentstore.Agent{Name: "live-agent"})

				return dir
			},
			wantReason: "live-agent",
		},
		{
			name: "an archived agent's directory",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
				fixture.archive(t, fixture.own(t, dir, agentstore.Agent{Name: "archived-agent"}).ID)

				return dir
			},
			wantReason: "archived-agent",
		},
		{
			name: "a directory whose agent's cleanup failed while git can still read it",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
				fixture.own(t, dir, agentstore.Agent{Name: "vetoed-agent", State: agentstore.StateFailedToCleanup})

				return dir
			},
			wantReason: "vetoed-agent",
		},
		{
			name: "a directory another agent works in",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
				fixture.occupy(t, dir, "child-agent")

				return dir
			},
			wantReason: "child-agent",
		},
		{
			name: "a directory an agent works somewhere under",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
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

				return fixture.cut(t)
			},
			wantReason: "could not list the worktrees",
			wantFailed: true,
		},
		{
			name: "a directory that could not be removed",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
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

// TestRemoveTakesADirectoryWhoseRowFailedToCleanUp covers the one owned directory the
// remover accepts, a cleanup that failed on a registration git cannot read.
func TestRemoveTakesADirectoryWhoseRowFailedToCleanUp(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	require.NoError(t, os.RemoveAll(filepath.Join(fixture.repo, ".git", "worktrees", filepath.Base(dir))))
	fixture.own(t, dir, agentstore.Agent{Name: "stuck", State: agentstore.StateFailedToCleanup})

	removal, err := fixture.newRemover().Remove(t.Context(), []string{dir})
	require.NoError(t, err)

	assert.Equal(t, []string{dir}, removal.Removed)
	assert.NoDirExists(t, dir)
}

// TestRemoveRefusesADirectoryHoldingAnotherWorktree covers a directory being left alone while
// a worktree sits inside it, whether the listing found that one under a workflow's directory
// or only the repository has it registered.
func TestRemoveRefusesADirectoryHoldingAnotherWorktree(t *testing.T) {
	fixture := newFixture(t)
	listed := fixture.cut(t)
	fixture.stores.SeedWorkflow(t, workflowstore.Config{Name: "nested", Worktree: workflowstore.WorktreeConfig{Dir: listed}})
	inner := filepath.Join(listed, "inner")
	write(t, inner, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/inner\n")
	registered := fixture.cut(t)
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
