package git

import (
	"os/exec"
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

	_, err := TakeSnapshot(t.Context(), TakeSnapshotParams{
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

	first, err := TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	time.Sleep(1100 * time.Millisecond)

	second, err := TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	assert.Equal(t, first.SHA, second.SHA, "the same worktree is the same round however long later")

	write(t, root, "kept.go", "package main\n\nfunc B() {}\n")

	moved, err := TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)
	assert.NotEqual(t, first.SHA, moved.SHA, "and a worktree that moved is a new one")
}

// TestReleaseDropsTheRef covers letting a round go, which leaves its commit to be collected.
func TestReleaseDropsTheRef(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc C() {}\n")

	_, err := TakeSnapshot(t.Context(), TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/five",
	})
	require.NoError(t, err)

	require.NoError(t, ReleaseRef(t.Context(), root, "refs/weave/rounds/five"))

	cmd := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", "refs/weave/rounds/five")
	require.Error(t, cmd.Run())
}

// TestFileAtReadsTheRoundRatherThanTheWorktree is what lets a review quote the code a comment was
// written about: the round holds its own copy, so what the agent does afterwards cannot change it.
func TestFileAtReadsTheRoundRatherThanTheWorktree(t *testing.T) {
	root := repo(t)
	write(t, root, "greet.go", "package main\n\nfunc greet() {}\n")

	snapshot, err := TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := ReadFileLines(t.Context(), root, snapshot.SHA, "greet.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main", "", "func greet() {}"}, lines)

	// The agent rewrites the file wholesale, which is the case the round exists for.
	write(t, root, "greet.go", "package main\n\nfunc greet(name string) {}\n")

	again, err := ReadFileLines(t.Context(), root, snapshot.SHA, "greet.go")
	require.NoError(t, err)
	assert.Equal(t, lines, again, "the round still holds what the comment was written about")

	_, err = ReadFileLines(t.Context(), root, snapshot.SHA, "never-existed.go")
	require.Error(t, err, "a path the round does not hold is an error the caller falls back from")
}
