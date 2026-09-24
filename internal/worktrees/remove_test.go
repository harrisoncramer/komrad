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

// TestRemoveRefusesWhatAnAgentAnswersFor covers the refusals: a live row, an archived row, a
// row whose cleanup failed while git can still read the directory, an agent working in the
// directory, an agent working somewhere under it, a path outside the worktree directories,
// and one that is not there at all.
func TestRemoveRefusesWhatAnAgentAnswersFor(t *testing.T) {
	fixture := newFixture(t)
	live := fixture.cut(t)
	fixture.own(t, live, agentstore.Agent{Name: "live-agent"})
	archived := fixture.cut(t)
	fixture.archive(t, fixture.own(t, archived, agentstore.Agent{Name: "archived-agent"}).ID)
	vetoed := fixture.cut(t)
	fixture.own(t, vetoed, agentstore.Agent{Name: "vetoed-agent", State: agentstore.StateFailedToCleanup})
	shared := fixture.cut(t)
	fixture.occupy(t, shared, "child-agent")
	deep := fixture.cut(t)
	fixture.occupy(t, filepath.Join(deep, "packages", "api"), "under-agent")
	outside := t.TempDir()

	removal, err := fixture.newRemover().Remove(t.Context(), []string{live, archived, vetoed, shared, deep, outside, filepath.Join(fixture.shared, "missing")})
	require.NoError(t, err)

	assert.Empty(t, removal.Removed)
	require.Len(t, removal.Refused, 7)
	assert.Contains(t, removal.Refused[0].Reason, "live-agent")
	assert.Contains(t, removal.Refused[1].Reason, "archived-agent")
	assert.Contains(t, removal.Refused[2].Reason, "vetoed-agent")
	assert.Contains(t, removal.Refused[3].Reason, "child-agent")
	assert.Contains(t, removal.Refused[4].Reason, "under-agent")
	assert.Contains(t, removal.Refused[5].Reason, "not under the worktree directories")
	assert.False(t, removal.Refused[5].Failed)
	assert.Contains(t, removal.Refused[6].Reason, "failed to resolve")
	assert.True(t, removal.Refused[6].Failed, "a path that could not be looked up is a step that broke")
	assert.DirExists(t, live)
	assert.DirExists(t, archived)
	assert.DirExists(t, vetoed)
	assert.DirExists(t, shared)
	assert.DirExists(t, deep)
	assert.Empty(t, fixture.git.Pruned)
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

// TestRemoveMarksAStepThatBroke covers the two refusals that are not verdicts about the
// directory: the repository's registrations could not be read, and the directory could not be
// removed.
func TestRemoveMarksAStepThatBroke(t *testing.T) {
	fixture := newFixture(t)
	unlisted := fixture.cut(t)
	fixture.git.ListErr = errors.New("git is not on the path")

	removal, err := fixture.newRemover().Remove(t.Context(), []string{unlisted})
	require.NoError(t, err)

	require.Len(t, removal.Refused, 1)
	assert.Contains(t, removal.Refused[0].Reason, "could not list the worktrees")
	assert.True(t, removal.Refused[0].Failed)
	assert.DirExists(t, unlisted)

	fixture.git.ListErr = nil
	held := fixture.cut(t)
	require.NoError(t, os.Chmod(fixture.shared, 0o500))
	t.Cleanup(func() { assert.NoError(t, os.Chmod(fixture.shared, 0o700)) })

	removal, err = fixture.newRemover().Remove(t.Context(), []string{held})
	require.NoError(t, err)

	require.Len(t, removal.Refused, 1)
	assert.Contains(t, removal.Refused[0].Reason, "could not remove it")
	assert.True(t, removal.Refused[0].Failed)
}
