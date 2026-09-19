package worktrees

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	worktreestore "weave/internal/store/worktree"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListReadsADirectoryWithoutGit covers the repository, the branch and the readability of
// a worktree coming off its own files, and the agent row that answers for it.
func TestListReadsADirectoryWithoutGit(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "aaaa")
	archived := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	f.own("aaaa", "first", &archived)
	f.sizes.sizes[dir] = worktreestore.Size{Dir: dir, Bytes: 42, CountedAt: archived}

	listing, err := f.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 1)
	tree := listing.Worktrees[0]
	assert.Equal(t, dir, tree.Dir)
	assert.Equal(t, f.repo, tree.RepoDir)
	assert.Equal(t, "agent/aaaa", tree.Branch)
	assert.True(t, tree.Readable)
	require.NotNil(t, tree.Agent)
	assert.Equal(t, "first", tree.Agent.Name)
	assert.Equal(t, agentstore.StateIdle, tree.Agent.State)
	assert.Equal(t, &archived, tree.Agent.ArchivedAt)
	assert.Equal(t, "flow", tree.Agent.WorkflowID)
	require.NotNil(t, tree.Size)
	assert.Equal(t, int64(42), tree.Size.Bytes)
	assert.Equal(t, []string{f.shared}, listing.Dirs)
	assert.Empty(t, listing.UnreadableDirs)
}

// TestListReadsADetachedHead covers a HEAD holding a hash rather than a ref naming no branch.
func TestListReadsADetachedHead(t *testing.T) {
	f := newFixture(t)
	f.cut(t, "aaaa")
	write(t, filepath.Join(f.repo, ".git", "worktrees", "aaaa"), "HEAD", "0123456789abcdef0123456789abcdef01234567\n")

	listing, err := f.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 1)
	assert.True(t, listing.Worktrees[0].Readable)
	assert.Empty(t, listing.Worktrees[0].Branch)
}

// TestListReadsAGoneRegistration covers a .git file naming a gitdir that is no longer there:
// the repository is still read off the path, and nothing else is.
func TestListReadsAGoneRegistration(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.shared, "bbbb")
	write(t, dir, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/bbbb\n")

	listing, err := f.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 1)
	tree := listing.Worktrees[0]
	assert.False(t, tree.Readable)
	assert.Equal(t, "/var/folders/xx/T/repo", tree.RepoDir)
	assert.Empty(t, tree.Branch)
	assert.Nil(t, tree.Agent)
}

// TestListReadsADirectoryWithNoGitFile covers a directory that is not a worktree at all
// still being listed, since it is taking the disk either way, and a file beside it not.
func TestListReadsADirectoryWithNoGitFile(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.shared, "cccc"), 0o755))
	write(t, f.shared, "a-file", "not a directory\n")

	listing, err := f.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 1)
	assert.Empty(t, listing.Worktrees[0].RepoDir)
	assert.False(t, listing.Worktrees[0].Readable)
}

// TestListNamesTheChildrenWorkingInADirectory covers a child row keyed on its cwd being listed
// under the row the directory was cut for, and sorted by name.
func TestListNamesTheChildrenWorkingInADirectory(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "dddd")
	f.own("dddd", "parent", nil)
	parent := "dddd"
	for _, name := range []string{"zed-reviewer", "amy-reviewer"} {
		f.owners.rows = append(f.owners.rows, agentstore.AgentDir{
			ID:            name,
			Name:          name,
			Cwd:           dir,
			State:         agentstore.StateThinking,
			ParentAgentID: &parent,
		})
	}

	listing, err := f.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 1)
	require.NotNil(t, listing.Worktrees[0].Agent)
	assert.Equal(t, "parent", listing.Worktrees[0].Agent.Name)
	require.Len(t, listing.Worktrees[0].Children, 2)
	assert.Equal(t, "amy-reviewer", listing.Worktrees[0].Children[0].Name)
	assert.Equal(t, "zed-reviewer", listing.Worktrees[0].Children[1].Name)
}

// TestListWalksEachWorkflowsOwnDirectory covers a workflow's own worktree directory being
// walked beside the shared one, a missing one being skipped, and both being named once.
func TestListWalksEachWorkflowsOwnDirectory(t *testing.T) {
	f := newFixture(t)
	own := filepath.Join(t.TempDir(), "own")
	require.NoError(t, os.MkdirAll(filepath.Join(own, "eeee"), 0o755))
	f.flows.dirs = []string{own, own, filepath.Join(t.TempDir(), "missing"), " "}
	f.cut(t, "ffff")

	listing, err := f.lister.List(t.Context())
	require.NoError(t, err)

	assert.Len(t, listing.Worktrees, 2)
	assert.Len(t, listing.Dirs, 3)
	assert.Empty(t, listing.UnreadableDirs)
}

// TestListNamesADirectoryItCannotRead covers the shared directory being unreadable being
// reported rather than read as empty.
func TestListNamesADirectoryItCannotRead(t *testing.T) {
	f := newFixture(t)
	unreadable := filepath.Join(filepath.Dir(f.shared), "not-a-dir")
	write(t, filepath.Dir(f.shared), "not-a-dir", "x\n")
	f.lister.shared = func() (string, error) { return unreadable, nil }

	listing, err := f.lister.List(t.Context())
	require.NoError(t, err)

	assert.Empty(t, listing.Worktrees)
	assert.Equal(t, []string{unreadable}, listing.UnreadableDirs)
}
