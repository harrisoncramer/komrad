package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMaterialiseIsTheRoundRatherThanTheWorktree is what the review screen's editor depends on:
// the checkout holds the code as the snapshot froze it, however far the agent has written past it.
func TestMaterialiseIsTheRoundRatherThanTheWorktree(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc Frozen() {}\n")

	snapshot, err := TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	write(t, root, "kept.go", "package main\n\nfunc MovedOn() {}\n")

	at := filepath.Join(t.TempDir(), "round")
	require.NoError(t, Materialise(t.Context(), root, snapshot.SHA, at))

	held, err := os.ReadFile(filepath.Join(at, "kept.go"))
	require.NoError(t, err)

	assert.Contains(t, string(held), "Frozen")
	assert.NotContains(t, string(held), "MovedOn")

	head, err := Run(t.Context(), at, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, snapshot.SHA, head)

	assert.Nil(t, GetCurrentBranch(t.Context(), at), "the checkout is detached, so nothing can be pushed from it")
}

// TestMaterialiseMovesACheckoutAlreadyThere covers the second round of the same agent, which
// reuses the directory rather than leaving one checkout per round on disk.
func TestMaterialiseMovesACheckoutAlreadyThere(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc First() {}\n")

	first, err := TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)
	require.NoError(t, HoldRef(t.Context(), root, "refs/weave/rounds/first", first.SHA))

	at := filepath.Join(t.TempDir(), "round")
	require.NoError(t, Materialise(t.Context(), root, first.SHA, at))

	write(t, root, "kept.go", "package main\n\nfunc Second() {}\n")

	second, err := TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)
	require.NoError(t, Materialise(t.Context(), root, second.SHA, at))

	held, err := os.ReadFile(filepath.Join(at, "kept.go"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "Second")

	trees, err := Run(t.Context(), root, "worktree", "list")
	require.NoError(t, err)
	assert.Equal(t, 1, countWorktrees(t, trees, at))
}

// TestMaterialiseKeepsWhatTheReviewerWrote covers the round being reopened while an edit of it is
// still held, which must not throw the edit away.
func TestMaterialiseKeepsWhatTheReviewerWrote(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n")

	snapshot, err := TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	at := filepath.Join(t.TempDir(), "round")
	require.NoError(t, Materialise(t.Context(), root, snapshot.SHA, at))
	write(t, at, "kept.go", "package main\n\nfunc TheReviewersIdea() {}\n")

	require.NoError(t, Materialise(t.Context(), root, snapshot.SHA, at))

	held, err := os.ReadFile(filepath.Join(at, "kept.go"))
	require.NoError(t, err)
	assert.Contains(t, string(held), "TheReviewersIdea")
}

// TestReapTakesTheRecordWithTheDirectory covers what makes the next checkout possible: git's own
// record of the worktree goes with the files, and a directory that was never there is no error.
func TestReapTakesTheRecordWithTheDirectory(t *testing.T) {
	root := repo(t)

	snapshot, err := TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	at := filepath.Join(t.TempDir(), "round")
	require.NoError(t, Materialise(t.Context(), root, snapshot.SHA, at))
	require.NoError(t, Reap(t.Context(), root, at))

	assert.NoDirExists(t, at)

	trees, err := Run(t.Context(), root, "worktree", "list")
	require.NoError(t, err)
	assert.Equal(t, 0, countWorktrees(t, trees, at))

	require.NoError(t, Reap(t.Context(), root, at))
}

// countWorktrees is how many of the worktrees git lists are the one at a path, resolved first
// since a temporary directory reaches git through a symlink and comes back as what it points at.
func countWorktrees(t *testing.T, listing, at string) int {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(filepath.Dir(at))
	if err != nil {
		resolved = filepath.Dir(at)
	}

	count := 0
	for _, line := range strings.Split(listing, "\n") {
		if strings.HasPrefix(line, filepath.Join(resolved, filepath.Base(at))+" ") {
			count++
		}
	}

	return count
}
