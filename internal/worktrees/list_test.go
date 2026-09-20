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
// still being listed, and a file beside it not.
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

// TestListNamesTheOccupantsOfADirectory covers every row whose cwd is the directory, however
// the path is spelled, being listed beside the row it was cut for, sorted by name.
func TestListNamesTheOccupantsOfADirectory(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "dddd")
	f.own("dddd", "parent", nil)
	f.owners.rows = append(f.owners.rows,
		agentstore.AgentDir{
			ID:    "zed",
			Name:  "zed-reviewer",
			Cwd:   dir,
			State: agentstore.StateThinking,
		},
		agentstore.AgentDir{
			ID:    "amy",
			Name:  "amy-alone",
			Cwd:   dir + string(filepath.Separator),
			State: agentstore.StateIdle,
		},
	)

	listing, err := f.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 1)
	require.NotNil(t, listing.Worktrees[0].Agent)
	assert.Equal(t, "parent", listing.Worktrees[0].Agent.Name)
	require.Len(t, listing.Worktrees[0].Occupants, 2)
	assert.Equal(t, "amy-alone", listing.Worktrees[0].Occupants[0].Name)
	assert.Equal(t, "zed-reviewer", listing.Worktrees[0].Occupants[1].Name)
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

// TestListJoinsTheStoredSize covers a directory carrying the measurement stored for it, and one
// the loop has not reached carrying none.
func TestListJoinsTheStoredSize(t *testing.T) {
	f := newFixture(t)
	measured := f.cut(t, "aaaa")
	f.cut(t, "bbbb")
	f.sizes.stored[measured] = worktreestore.Size{
		Dir:        measured,
		Bytes:      4096,
		MeasuredAt: f.now,
	}

	listing, err := f.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 2)
	require.NotNil(t, listing.Worktrees[0].Size)
	assert.Equal(t, int64(4096), listing.Worktrees[0].Size.Bytes)
	assert.Equal(t, f.now, listing.Worktrees[0].Size.MeasuredAt)
	assert.Nil(t, listing.Worktrees[1].Size)
}

// TestListReadsWhenADirectoryWasLastActive covers each rule for the last activity: the later
// of an agent's state change and its archiving, else the .git file's mtime, else the
// directory's own.
func TestListReadsWhenADirectoryWasLastActive(t *testing.T) {
	changed := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	archived := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	gitTouched := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	dirTouched := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		lay  func(t *testing.T, f *fixture) string
		want time.Time
	}{
		{
			name: "an agent archived after its last state change",
			lay: func(t *testing.T, f *fixture) string {
				dir := f.cut(t, "aaaa")
				f.own("aaaa", "first", &archived)
				f.owners.rows[0].StateChangedAt = changed

				return dir
			},
			want: archived,
		},
		{
			name: "an agent whose state changed after it was archived",
			lay: func(t *testing.T, f *fixture) string {
				dir := f.cut(t, "aaaa")
				f.own("aaaa", "first", &changed)
				f.owners.rows[0].StateChangedAt = archived

				return dir
			},
			want: archived,
		},
		{
			name: "a live agent reads its state change",
			lay: func(t *testing.T, f *fixture) string {
				dir := f.cut(t, "aaaa")
				f.own("aaaa", "first", nil)
				f.owners.rows[0].StateChangedAt = changed

				return dir
			},
			want: changed,
		},
		{
			name: "no agent reads the .git file",
			lay: func(t *testing.T, f *fixture) string {
				dir := f.cut(t, "aaaa")
				require.NoError(t, os.Chtimes(filepath.Join(dir, ".git"), gitTouched, gitTouched))
				require.NoError(t, os.Chtimes(dir, dirTouched, dirTouched))

				return dir
			},
			want: gitTouched,
		},
		{
			name: "no .git file reads the directory",
			lay: func(t *testing.T, f *fixture) string {
				dir := filepath.Join(f.shared, "cccc")
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.Chtimes(dir, dirTouched, dirTouched))

				return dir
			},
			want: dirTouched,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			dir := tt.lay(t, f)

			listing, err := f.lister.List(t.Context())
			require.NoError(t, err)

			require.Len(t, listing.Worktrees, 1)
			assert.Equal(t, dir, listing.Worktrees[0].Dir)
			assert.True(t, listing.Worktrees[0].LastActiveAt.Equal(tt.want), "got %s, want %s", listing.Worktrees[0].LastActiveAt, tt.want)
		})
	}
}
