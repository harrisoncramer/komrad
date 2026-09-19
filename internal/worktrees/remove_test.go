package worktrees

import (
	"path/filepath"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	worktreestore "weave/internal/store/worktree"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveTakesAnUnownedDirectoryAndPrunesIt covers a readable worktree nothing answers
// for going, its repository pruned, its count dropped and its bytes reported.
func TestRemoveTakesAnUnownedDirectoryAndPrunesIt(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "aaaa")
	f.sizes.sizes[dir] = worktreestore.Size{Dir: dir, Bytes: 77, CountedAt: time.Now()}

	removal, err := f.remover().Remove(t.Context(), []string{dir})
	require.NoError(t, err)

	assert.Equal(t, []string{dir}, removal.Removed)
	assert.Empty(t, removal.Refused)
	assert.Equal(t, int64(77), removal.FreedBytes)
	assert.NoDirExists(t, dir)
	assert.NotContains(t, f.sizes.sizes, dir)
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
// child working in the directory, a path outside the worktree directories, and one that is
// not there at all.
func TestRemoveRefusesWhatAnAgentAnswersFor(t *testing.T) {
	f := newFixture(t)
	live := f.cut(t, "live")
	f.own("live", "live-agent", nil)
	archived := f.cut(t, "archived")
	at := time.Now()
	f.own("archived", "archived-agent", &at)
	shared := f.cut(t, "shared")
	parent := "gone-parent"
	f.owners.rows = append(f.owners.rows, agentstore.AgentDir{
		ID: "child", Name: "child-agent", Cwd: shared, State: agentstore.StateIdle, ParentAgentID: &parent,
	})
	outside := t.TempDir()

	removal, err := f.remover().Remove(t.Context(), []string{live, archived, shared, outside, filepath.Join(f.shared, "missing")})
	require.NoError(t, err)

	assert.Empty(t, removal.Removed)
	require.Len(t, removal.Refused, 5)
	assert.Contains(t, removal.Refused[0].Reason, "live-agent")
	assert.Contains(t, removal.Refused[1].Reason, "archived-agent")
	assert.Contains(t, removal.Refused[2].Reason, "child-agent")
	assert.Contains(t, removal.Refused[3].Reason, "not under the worktree directories")
	assert.Contains(t, removal.Refused[4].Reason, "failed to resolve")
	assert.DirExists(t, live)
	assert.DirExists(t, archived)
	assert.DirExists(t, shared)
	assert.Empty(t, f.git.pruned)
}

// TestRemoveTakesADirectoryWhoseRowFailedToCleanUp covers the one owned directory the
// remover accepts, since its own delete can never clear it.
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
