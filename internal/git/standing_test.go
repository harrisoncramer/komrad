//go:build integration

package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseChangesSplitsTrackedFromUntracked pins the porcelain being read record by record,
// with a rename's old path skipped rather than listed as a change of its own.
func TestParseChangesSplitsTrackedFromUntracked(t *testing.T) {
	out := " M a.go\x00A  b.go\x00R  new.go\x00old.go\x00?? notes.md\x00D  gone.go\x00"

	changes := parseChanges(out)

	assert.Equal(t, []string{"a.go", "b.go", "new.go", "gone.go"}, changes.Modified)
	assert.Equal(t, []string{"notes.md"}, changes.Untracked)
}

// TestParseChangesOfACleanTree pins an empty listing reading as nothing rather than as one
// empty path.
func TestParseChangesOfACleanTree(t *testing.T) {
	changes := parseChanges("")

	assert.Empty(t, changes.Modified)
	assert.Empty(t, changes.Untracked)
}

// TestReadChangesReadsTheCheckout covers the tracked and untracked halves against a real tree.
func TestReadChangesReadsTheCheckout(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nvar changed = true\n")
	write(t, root, "scratch.txt", "untracked\n")

	changes, err := client.ReadChanges(t.Context(), root)
	require.NoError(t, err)

	assert.Equal(t, []string{"kept.go"}, changes.Modified)
	assert.Equal(t, []string{"scratch.txt"}, changes.Untracked)
}

// TestCountAheadBehind covers a branch two commits past a base that has itself moved one.
func TestCountAheadBehind(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "one.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "one")
	write(t, root, "two.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "two")
	run(t, root, "checkout", "-q", "main")
	write(t, root, "base.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "base moved")
	run(t, root, "checkout", "-q", "feature")

	ahead, behind, err := client.CountAheadBehind(t.Context(), root, "main")
	require.NoError(t, err)

	assert.Equal(t, 2, ahead)
	assert.Equal(t, 1, behind)
}

// TestIsBranchHeldElsewhere covers a branch whose tip only it reaches, and the same branch
// once main has merged it.
func TestIsBranchHeldElsewhere(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	write(t, root, "one.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-q", "-m", "one")

	held, err := client.IsBranchHeldElsewhere(t.Context(), root, "feature")
	require.NoError(t, err)
	assert.False(t, held)

	run(t, root, "checkout", "-q", "main")
	run(t, root, "merge", "-q", "--ff-only", "feature")

	held, err = client.IsBranchHeldElsewhere(t.Context(), root, "feature")
	require.NoError(t, err)
	assert.True(t, held)
}

// TestListCommitsSince covers the branch's own commits coming back newest first and capped.
func TestListCommitsSince(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-q", "-b", "feature")
	for _, name := range []string{"one", "two", "three"} {
		write(t, root, name+".go", "package main\n")
		run(t, root, "add", "-A")
		run(t, root, "commit", "-q", "-m", name)
	}

	commits, err := client.ListCommitsSince(t.Context(), root, "main", 2)
	require.NoError(t, err)

	require.Len(t, commits, 2)
	assert.Equal(t, "three", commits[0].Subject)
	assert.Equal(t, "two", commits[1].Subject)
	assert.Len(t, commits[0].SHA, 40)
	assert.False(t, commits[0].CommittedAt.IsZero())
}

// TestPruneWorktreesDropsAGoneRegistration covers a directory removed by hand leaving a
// registration that prune takes away.
func TestPruneWorktreesDropsAGoneRegistration(t *testing.T) {
	root := repo(t)
	tree := filepath.Join(t.TempDir(), "tree")
	run(t, root, "worktree", "add", "-q", "-b", "feature", tree)
	require.NoError(t, os.RemoveAll(tree))

	listed, err := client.ListWorktrees(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, listed, 2)

	require.NoError(t, client.PruneWorktrees(t.Context(), root))

	listed, err = client.ListWorktrees(t.Context(), root)
	require.NoError(t, err)
	assert.Len(t, listed, 1)
}
