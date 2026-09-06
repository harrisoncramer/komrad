package diff

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

// TestTakeHoldsCommittedAndUncommittedWorkAlike pins what a round covers, since an agent mid-turn
// has committed some of its work and left the rest in the worktree.
func TestTakeHoldsCommittedAndUncommittedWorkAlike(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-b", "agent/one")

	write(t, root, "committed.go", "package main\n\nfunc A() {}\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "work")

	write(t, root, "kept.go", "package main\n\nfunc B() {}\n")
	write(t, root, "untracked.go", "package main\n")

	snapshot, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main", Ref: "refs/weave/rounds/one"})
	require.NoError(t, err)

	patch, err := Load(t.Context(), LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"committed.go", "kept.go", "untracked.go"}, changed(patch))
}

func changed(patch *Patch) []string {
	found := make([]string, 0, len(patch.Files))
	for _, file := range patch.Files {
		if len(file.Hunks) > 0 || file.Content != "" {
			found = append(found, file.Path())
		}
	}

	return found
}

// TestTakeLeavesTheAgentsIndexAndWorktreeAlone is the whole reason the snapshot is built in an
// index of its own, since a round taken under a working agent must not disturb it.
func TestTakeLeavesTheAgentsIndexAndWorktreeAlone(t *testing.T) {
	root := repo(t)
	write(t, root, "staged.go", "package main\n")
	run(t, root, "add", "staged.go")
	write(t, root, "loose.go", "package main\n")

	before := capture(t, root, "status", "--porcelain")
	head := capture(t, root, "rev-parse", "HEAD")

	_, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main", Ref: "refs/weave/rounds/two"})
	require.NoError(t, err)

	assert.Equal(t, before, capture(t, root, "status", "--porcelain"))
	assert.Equal(t, head, capture(t, root, "rev-parse", "HEAD"))
}

// TestTakeIsHeldByItsRefAndReadableAfterTheWorktreeMovesOn is what a round is for: the patch reads
// the same however far the agent has moved on since.
func TestTakeIsHeldByItsRefAndReadableAfterTheWorktreeMovesOn(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc First() {}\n")

	snapshot, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main", Ref: "refs/weave/rounds/three"})
	require.NoError(t, err)

	write(t, root, "kept.go", "package main\n\nfunc Second() {}\n")
	require.NoError(t, os.Remove(filepath.Join(root, "proto", "gen", "api.pb.go")))

	patch, err := Load(t.Context(), LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
	require.NoError(t, err)

	require.Len(t, patch.Files, 1)
	assert.Equal(t, "kept.go", patch.Files[0].Path())
	assert.Contains(t, patch.Files[0].NewSource, "func First()")
	assert.Equal(t, snapshot.SHA, capture(t, root, "rev-parse", "refs/weave/rounds/three"))
}

// TestTakeIgnoresWhatGitIgnores pins that a snapshot obeys the repository's own ignores rather than
// staging everything on disk.
func TestTakeIgnoresWhatGitIgnores(t *testing.T) {
	root := repo(t)
	write(t, root, ".gitignore", ".weave/\n")
	write(t, root, ".weave/reviews/round-1.md", "a review\n")

	snapshot, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main", Ref: "refs/weave/rounds/four"})
	require.NoError(t, err)

	patch, err := Load(t.Context(), LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
	require.NoError(t, err)

	assert.NotContains(t, changed(patch), ".weave/reviews/round-1.md")
}

// TestTakingTheSameTreeTwiceIsTheSameRound is why the commit's dates are pinned: a snapshot that
// carried the clock would hash differently every time, so reopening a diff a second later would
// read as a new round and lose every mark made against the first.
func TestTakingTheSameTreeTwiceIsTheSameRound(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc A() {}\n")

	first, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	time.Sleep(1100 * time.Millisecond)

	second, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	assert.Equal(t, first.SHA, second.SHA, "the same worktree is the same round however long later")

	write(t, root, "kept.go", "package main\n\nfunc B() {}\n")

	moved, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)
	assert.NotEqual(t, first.SHA, moved.SHA, "and a worktree that moved is a new one")
}

// TestReleaseDropsTheRef covers letting a round go, which leaves its commit to be collected.
func TestReleaseDropsTheRef(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc C() {}\n")

	_, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main", Ref: "refs/weave/rounds/five"})
	require.NoError(t, err)

	require.NoError(t, Release(t.Context(), root, "refs/weave/rounds/five"))

	cmd := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", "refs/weave/rounds/five")
	assert.Error(t, cmd.Run())
}

// TestTakeExcludesNothingItself pins where the workflow's excludes are applied, which is the diff
// rather than the snapshot, so a round holds the whole tree whatever the reader is shown.
func TestTakeExcludesNothingItself(t *testing.T) {
	root := repo(t)
	write(t, root, "proto/gen/api.pb.go", "package gen\n\nfunc Gen() {}\n")

	snapshot, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main", Ref: "refs/weave/rounds/six"})
	require.NoError(t, err)

	// The round holds the excluded file whole, which is what makes an exclude a view of a
	// snapshot rather than a decision baked into it.
	lines, err := FileAt(t.Context(), root, snapshot.SHA, "proto/gen/api.pb.go")
	require.NoError(t, err)
	assert.Contains(t, lines, "func Gen() {}")

	// The reader is shown none of it, and is told that rather than told the agent did nothing,
	// since it wrote a file and the exclude is the only reason there is nothing on screen.
	_, err = Load(t.Context(), LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
		Exclude:     []string{"proto/gen"},
	})
	require.ErrorIs(t, err, ErrAllExcluded)
	require.NotErrorIs(t, err, ErrNoChanges)
}

// TestLoadSaysNothingChangedOnlyWhenNothingDid pins the other half of that: a worktree that really
// has not moved reads as nothing to review whether or not the workflow excludes anything.
func TestLoadSaysNothingChangedOnlyWhenNothingDid(t *testing.T) {
	root := repo(t)

	snapshot, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	for _, exclude := range [][]string{nil, {"proto/gen"}} {
		_, err = Load(t.Context(), LoadParams{
			Dir:         root,
			BaseSHA:     snapshot.BaseSHA,
			SnapshotSHA: snapshot.SHA,
			Exclude:     exclude,
		})
		require.ErrorIs(t, err, ErrNoChanges)
	}
}

// TestFileAtReadsTheRoundRatherThanTheWorktree is what lets a review quote the code a comment was
// written about: the round holds its own copy, so what the agent does afterwards cannot change it.
func TestFileAtReadsTheRoundRatherThanTheWorktree(t *testing.T) {
	root := repo(t)
	write(t, root, "greet.go", "package main\n\nfunc greet() {}\n")

	snapshot, err := Take(t.Context(), SnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := FileAt(t.Context(), root, snapshot.SHA, "greet.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main", "", "func greet() {}"}, lines)

	// The agent rewrites the file wholesale, which is the case the round exists for.
	write(t, root, "greet.go", "package main\n\nfunc greet(name string) {}\n")

	again, err := FileAt(t.Context(), root, snapshot.SHA, "greet.go")
	require.NoError(t, err)
	assert.Equal(t, lines, again, "the round still holds what the comment was written about")

	_, err = FileAt(t.Context(), root, snapshot.SHA, "never-existed.go")
	require.Error(t, err, "a path the round does not hold is an error the caller falls back from")
}
