package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func capture(t *testing.T, root string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	return strings.TrimSpace(string(out))
}

// TestTakeLeavesTheAgentsIndexAndWorktreeAlone is the whole reason the snapshot is built in an
// index of its own.
func TestTakeLeavesTheAgentsIndexAndWorktreeAlone(t *testing.T) {
	root := repo(t)
	write(t, root, "staged.go", "package main\n")
	run(t, root, "add", "staged.go")
	write(t, root, "loose.go", "package main\n")

	before := capture(t, root, "status", "--porcelain")
	head := capture(t, root, "rev-parse", "HEAD")

	_, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/two",
	})
	require.NoError(t, err)

	assert.Equal(t, before, capture(t, root, "status", "--porcelain"))
	assert.Equal(t, head, capture(t, root, "rev-parse", "HEAD"))
}

// TestTakingTheSameTreeTwiceIsTheSameRound is why the commit's dates are pinned: a snapshot that
// carried the clock would hash differently every time, so reopening a diff a second later would
// read as a new round and lose every mark made against the first.
func TestTakingTheSameTreeTwiceIsTheSameRound(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc A() {}\n")

	first, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	time.Sleep(1100 * time.Millisecond)

	second, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	assert.Equal(t, first.SHA, second.SHA, "the same worktree is the same round however long later")

	write(t, root, "kept.go", "package main\n\nfunc B() {}\n")

	moved, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)
	assert.NotEqual(t, first.SHA, moved.SHA, "and a worktree that moved is a new one")
}

// TestReleaseDropsTheRef covers letting a round go, which leaves its commit to be collected.
func TestReleaseDropsTheRef(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc C() {}\n")

	_, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/five",
	})
	require.NoError(t, err)

	require.NoError(t, client.ReleaseRef(t.Context(), root, "refs/weave/rounds/five"))

	cmd := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", "refs/weave/rounds/five")
	require.Error(t, cmd.Run())
}

// TestFileAtReadsTheRoundRatherThanTheWorktree is what lets a review quote the code a comment was
// written about: the round holds its own copy, so what the agent does afterwards cannot change it.
func TestFileAtReadsTheRoundRatherThanTheWorktree(t *testing.T) {
	root := repo(t)
	write(t, root, "greet.go", "package main\n\nfunc greet() {}\n")

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "greet.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main", "", "func greet() {}"}, lines)

	// The agent rewrites the file wholesale, which is the case the round exists for.
	write(t, root, "greet.go", "package main\n\nfunc greet(name string) {}\n")

	again, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "greet.go")
	require.NoError(t, err)
	assert.Equal(t, lines, again, "the round still holds what the comment was written about")

	_, err = client.ReadFileLines(t.Context(), root, snapshot.SHA, "never-existed.go")
	require.Error(t, err, "a path the round does not hold is an error the caller falls back from")
}

// controlTree is the tree an index seeded from HEAD gives, which is how the snapshot was built
// before it read the agent's own index.
func controlTree(t *testing.T, root string) string {
	t.Helper()

	index := filepath.Join(t.TempDir(), "control.index")
	env := append(append(os.Environ(), snapshotIdentity...), "GIT_INDEX_FILE="+index)

	for _, args := range [][]string{{"read-tree", "HEAD"}, {"add", "-A"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}

	cmd := exec.Command("git", "-C", root, "write-tree")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	return strings.TrimSpace(string(out))
}

// TestTheTreeIsTheSameHoweverTheIndexWasSeeded is the whole claim of reading the agent's index
// rather than HEAD, so the two are made to agree over a worktree holding one of everything.
func TestTheTreeIsTheSameHoweverTheIndexWasSeeded(t *testing.T) {
	root := repo(t)

	write(t, root, "staged.go", "package main\n")
	run(t, root, "add", "staged.go")
	write(t, root, "staged.go", "package main\n\nfunc Later() {}\n")
	write(t, root, "loose.go", "package main\n")
	write(t, root, "deep/nested/new.go", "package nested\n")
	write(t, root, "intent.go", "package main\n")
	run(t, root, "add", "-N", "intent.go")
	require.NoError(t, os.Remove(filepath.Join(root, "kept.go")))
	require.NoError(t, os.Chmod(filepath.Join(root, "proto/gen/api.pb.go"), 0o755))
	require.NoError(t, os.Symlink("loose.go", filepath.Join(root, "link.go")))

	tree, err := client.ProbeTree(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, controlTree(t, root), tree)
}

// TestAnAssumeUnchangedFileIsStillHashed is why the agent's index is read only after asking about
// its flags: it tells git to trust a recorded time over the file, and a round that did so would
// leave the agent's edit out of the diff it is reviewed by.
func TestAnAssumeUnchangedFileIsStillHashed(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--assume-unchanged", "kept.go")
	write(t, root, "kept.go", "package main\n\nfunc Edited() {}\n")

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "kept.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main", "", "func Edited() {}"}, lines)
}

// TestASkipWorktreeFileIsKept covers a sparse checkout, where a file the agent never had on disk
// is one it has not deleted either.
func TestASkipWorktreeFileIsKept(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--skip-worktree", "kept.go")
	require.NoError(t, os.Remove(filepath.Join(root, "kept.go")))

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "kept.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main"}, lines)
}

// TestASnapshotSurvivesAnUnreadableIndex is the fallback: an index the agent's own git would
// refuse is no reason for the panel to refuse a diff.
func TestASnapshotSurvivesAnUnreadableIndex(t *testing.T) {
	root := repo(t)
	write(t, root, "loose.go", "package main\n")

	path := filepath.Join(root, capture(t, root, "rev-parse", "--git-path", "index"))
	require.NoError(t, os.WriteFile(path, []byte("not an index at all"), 0o644))

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "loose.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main"}, lines)
}
