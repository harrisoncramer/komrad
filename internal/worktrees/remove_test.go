package worktrees

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveTakesAnUnownedDirectoryAndPrunesIt covers a readable worktree nothing answers
// for going and its repository being pruned.
func TestRemoveTakesAnUnownedDirectoryAndPrunesIt(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "aaaa")

	removal, err := f.remover().Remove(t.Context(), []string{dir})
	require.NoError(t, err)

	assert.Equal(t, []string{dir}, removal.Removed)
	assert.Empty(t, removal.Refused)
	assert.NoDirExists(t, dir)
	assert.Equal(t, []string{f.repo}, f.git.pruned)
}

// TestRemoveTakesAGoneRegistrationWithoutPruning covers a directory git cannot read going
// without a prune being attempted against a repository that is not there.
func TestRemoveTakesAGoneRegistrationWithoutPruning(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.shared, "bbbb")
	write(t, dir, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/bbbb\n")

	removal, err := f.remover().Remove(t.Context(), []string{dir})
	require.NoError(t, err)

	assert.Equal(t, []string{dir}, removal.Removed)
	assert.NoDirExists(t, dir)
	assert.Empty(t, f.git.pruned)
}

// TestRemoveRefusesWhatAnAgentAnswersFor covers the refusals: a live row, an archived row, a
// row whose cleanup failed while git can still read the directory, an agent working in the
// directory, an agent working somewhere under it, a path outside the worktree directories,
// and one that is not there at all.
func TestRemoveRefusesWhatAnAgentAnswersFor(t *testing.T) {
	f := newFixture(t)
	live := f.cut(t, "live")
	f.own("live", "live-agent", nil)
	archived := f.cut(t, "archived")
	at := time.Now()
	f.own("archived", "archived-agent", &at)
	vetoed := f.cut(t, "vetoed")
	f.own("vetoed", "vetoed-agent", nil)
	f.owners.rows[2].State = agentstore.StateFailedToCleanup
	shared := f.cut(t, "shared")
	f.owners.rows = append(f.owners.rows, agentstore.AgentDir{
		ID:    "child",
		Name:  "child-agent",
		Cwd:   shared,
		State: agentstore.StateIdle,
	})
	deep := f.cut(t, "deep")
	f.owners.rows = append(f.owners.rows, agentstore.AgentDir{
		ID:    "under",
		Name:  "under-agent",
		Cwd:   filepath.Join(deep, "packages", "api"),
		State: agentstore.StateIdle,
	})
	outside := t.TempDir()

	removal, err := f.remover().Remove(t.Context(), []string{live, archived, vetoed, shared, deep, outside, filepath.Join(f.shared, "missing")})
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
	assert.Empty(t, f.git.pruned)
}

// TestRemoveTakesADirectoryWhoseRowFailedToCleanUp covers the one owned directory the
// remover accepts, a cleanup that failed on a registration git cannot read.
func TestRemoveTakesADirectoryWhoseRowFailedToCleanUp(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.shared, "cccc")
	write(t, dir, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/cccc\n")
	f.own("cccc", "stuck", nil)
	f.owners.rows[0].State = agentstore.StateFailedToCleanup

	removal, err := f.remover().Remove(t.Context(), []string{dir})
	require.NoError(t, err)

	assert.Equal(t, []string{dir}, removal.Removed)
	assert.NoDirExists(t, dir)
}

// TestRemoveRefusesADirectoryHoldingAnotherWorktree covers a directory being left alone while
// a worktree sits inside it, whether the listing found that one under a workflow's directory
// or only the repository has it registered.
func TestRemoveRefusesADirectoryHoldingAnotherWorktree(t *testing.T) {
	f := newFixture(t)
	listed := f.cut(t, "listed")
	f.flows.dirs = []string{listed}
	inner := filepath.Join(listed, "inner")
	write(t, inner, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/inner\n")
	registered := f.cut(t, "registered")
	byHand := filepath.Join(registered, "by-hand")
	write(t, byHand, ".git", "gitdir: "+filepath.Join(f.repo, ".git", "worktrees", "by-hand")+"\n")
	f.git.registered = []git.Worktree{{Dir: f.repo}, {Dir: byHand}}

	removal, err := f.remover().Remove(t.Context(), []string{listed, registered})
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
	f := newFixture(t)
	unlisted := f.cut(t, "unlisted")
	f.git.listErr = errors.New("git is not on the path")

	removal, err := f.remover().Remove(t.Context(), []string{unlisted})
	require.NoError(t, err)

	require.Len(t, removal.Refused, 1)
	assert.Contains(t, removal.Refused[0].Reason, "could not list the worktrees")
	assert.True(t, removal.Refused[0].Failed)
	assert.DirExists(t, unlisted)

	f.git.listErr = nil
	held := f.cut(t, "held")
	require.NoError(t, os.Chmod(f.shared, 0o500))
	t.Cleanup(func() { assert.NoError(t, os.Chmod(f.shared, 0o700)) })

	removal, err = f.remover().Remove(t.Context(), []string{held})
	require.NoError(t, err)

	require.Len(t, removal.Refused, 1)
	assert.Contains(t, removal.Refused[0].Reason, "could not remove it")
	assert.True(t, removal.Refused[0].Failed)
}
