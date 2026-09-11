package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"weave/internal/git"

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

// TestTakeHoldsCommittedAndUncommittedWorkAlike pins what a round covers, which is an agent's
// committed work and what it has left in the worktree.
func TestTakeHoldsCommittedAndUncommittedWorkAlike(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-b", "agent/one")

	write(t, root, "committed.go", "package main\n\nfunc A() {}\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "work")

	write(t, root, "kept.go", "package main\n\nfunc B() {}\n")
	write(t, root, "untracked.go", "package main\n")

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/one",
	})
	require.NoError(t, err)

	patch, err := Load(t.Context(), gitClient, LoadParams{
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

// TestTakeIsHeldByItsRefAndReadableAfterTheWorktreeMovesOn is what a round is for: the patch reads
// the same however far the agent has moved on since.
func TestTakeIsHeldByItsRefAndReadableAfterTheWorktreeMovesOn(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc First() {}\n")

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/three",
	})
	require.NoError(t, err)

	write(t, root, "kept.go", "package main\n\nfunc Second() {}\n")
	require.NoError(t, os.Remove(filepath.Join(root, "proto", "gen", "api.pb.go")))

	patch, err := Load(t.Context(), gitClient, LoadParams{
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

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/four",
	})
	require.NoError(t, err)

	patch, err := Load(t.Context(), gitClient, LoadParams{
		Dir:         root,
		BaseSHA:     snapshot.BaseSHA,
		SnapshotSHA: snapshot.SHA,
	})
	require.NoError(t, err)

	assert.NotContains(t, changed(patch), ".weave/reviews/round-1.md")
}

// TestTakeExcludesNothingItself pins where the workflow's excludes are applied, which is the diff
// rather than the snapshot, so a round holds the whole tree whatever the reader is shown.
func TestTakeExcludesNothingItself(t *testing.T) {
	root := repo(t)
	write(t, root, "proto/gen/api.pb.go", "package gen\n\nfunc Gen() {}\n")

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/weave/rounds/six",
	})
	require.NoError(t, err)

	// The round holds the excluded file whole, which is what makes an exclude a view of a
	// snapshot rather than a decision baked into it.
	lines, err := gitClient.ReadFileLines(t.Context(), root, snapshot.SHA, "proto/gen/api.pb.go")
	require.NoError(t, err)
	assert.Contains(t, lines, "func Gen() {}")

	// The reader is shown none of it, and is told that rather than told the agent did nothing.
	_, err = Load(t.Context(), gitClient, LoadParams{
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

	snapshot, err := gitClient.TakeSnapshot(t.Context(), git.TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	for _, exclude := range [][]string{nil, {"proto/gen"}} {
		_, err = Load(t.Context(), gitClient, LoadParams{
			Dir:         root,
			BaseSHA:     snapshot.BaseSHA,
			SnapshotSHA: snapshot.SHA,
			Exclude:     exclude,
		})
		require.ErrorIs(t, err, ErrNoChanges)
	}
}
