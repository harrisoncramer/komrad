//go:build integration

package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"komrad/lib/clock"

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

// TestIntegrationTakeLeavesTheAgentsIndexAndWorktreeAlone is the whole reason the snapshot is built in an
// index of its own.
func TestIntegrationTakeLeavesTheAgentsIndexAndWorktreeAlone(t *testing.T) {
	root := repo(t)
	write(t, root, "staged.go", "package main\n")
	run(t, root, "add", "staged.go")
	write(t, root, "loose.go", "package main\n")

	before := capture(t, root, "status", "--porcelain")
	head := capture(t, root, "rev-parse", "HEAD")

	_, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/komrad/rounds/two",
	})
	require.NoError(t, err)

	assert.Equal(t, before, capture(t, root, "status", "--porcelain"))
	assert.Equal(t, head, capture(t, root, "rev-parse", "HEAD"))
}

// TestIntegrationTakingTheSameTreeTwiceIsTheSameRound is why the commit's dates are pinned: a snapshot that
// carried the clock would hash differently every time, so reopening a diff a second later would
// read as a new round and lose every mark made against the first.
func TestIntegrationTakingTheSameTreeTwiceIsTheSameRound(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc A() {}\n")

	first, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	clock.Sleep(t.Context(), 1100*time.Millisecond)

	second, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	assert.Equal(t, first.SHA, second.SHA, "the same worktree is the same round however long later")

	write(t, root, "kept.go", "package main\n\nfunc B() {}\n")

	moved, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)
	assert.NotEqual(t, first.SHA, moved.SHA, "and a worktree that moved is a new one")
}

// TestIntegrationASnapshotIsRefusedPartWayThroughAMerge pins that a half-applied tree is never
// frozen as a round.
func TestIntegrationASnapshotIsRefusedPartWayThroughAMerge(t *testing.T) {
	root := repo(t)
	run(t, root, "checkout", "-b", "topic")
	write(t, root, "clash.go", "package topic\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "topic")
	run(t, root, "checkout", "main")
	write(t, root, "clash.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "main")
	run(t, root, "checkout", "topic")
	require.Error(t, exec.Command("git", "-C", root, "merge", "main").Run())

	_, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.ErrorIs(t, err, ErrSequenceInProgress)

	run(t, root, "merge", "--abort")

	_, err = client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)
}

// TestIntegrationReleaseDropsTheRef covers letting a round go, which leaves its commit to be collected.
func TestIntegrationReleaseDropsTheRef(t *testing.T) {
	root := repo(t)
	write(t, root, "kept.go", "package main\n\nfunc C() {}\n")

	_, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{
		Dir:  root,
		Base: "main",
		Ref:  "refs/komrad/rounds/five",
	})
	require.NoError(t, err)

	require.NoError(t, client.ReleaseRef(t.Context(), root, "refs/komrad/rounds/five"))

	cmd := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", "refs/komrad/rounds/five")
	require.Error(t, cmd.Run())
}

// TestIntegrationFileAtReadsTheRoundRatherThanTheWorktree is what lets a review quote the code a comment was
// written about: the round holds its own copy, so what the agent does afterwards cannot change it.
func TestIntegrationFileAtReadsTheRoundRatherThanTheWorktree(t *testing.T) {
	root := repo(t)
	write(t, root, "greet.go", "package main\n\nfunc greet() {}\n")

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "greet.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main", "", "func greet() {}"}, lines)

	// The agent rewrites the file wholesale.
	write(t, root, "greet.go", "package main\n\nfunc greet(name string) {}\n")

	again, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "greet.go")
	require.NoError(t, err)
	assert.Equal(t, lines, again, "the round still holds what the comment was written about")

	_, err = client.ReadFileLines(t.Context(), root, snapshot.SHA, "never-existed.go")
	require.Error(t, err, "a path the round does not hold is an error the caller falls back from")
}

// controlTree is the tree an index seeded from HEAD gives.
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

// TestIntegrationTheTreeIsTheSameHoweverTheIndexWasSeeded is the whole claim of reading the agent's index
// rather than HEAD, so the two are made to agree over a worktree holding one of everything.
func TestIntegrationTheTreeIsTheSameHoweverTheIndexWasSeeded(t *testing.T) {
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

// TestIntegrationAnAssumeUnchangedFileIsStillHashed is why the agent's index is read only after asking about
// its flags: it tells git to trust a recorded time over the file, and a round that did so would
// leave the agent's edit out of the diff it is reviewed by.
func TestIntegrationAnAssumeUnchangedFileIsStillHashed(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--assume-unchanged", "kept.go")
	write(t, root, "kept.go", "package main\n\nfunc Edited() {}\n")

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "kept.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main", "", "func Edited() {}"}, lines)
}

// TestIntegrationASkipWorktreeFileIsKept covers a sparse checkout, where a file the agent never had on disk
// is one it has not deleted either.
func TestIntegrationASkipWorktreeFileIsKept(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--skip-worktree", "kept.go")
	require.NoError(t, os.Remove(filepath.Join(root, "kept.go")))

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "kept.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main"}, lines)
}

// TestIntegrationASkipWorktreeFileOnDiskIsStillHashed is the same invariant for the other flag git reads an
// index entry over a file for.
func TestIntegrationASkipWorktreeFileOnDiskIsStillHashed(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--skip-worktree", "kept.go")
	write(t, root, "kept.go", "package main\n\nfunc Edited() {}\n")

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "kept.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main", "", "func Edited() {}"}, lines)
}

// TestIntegrationAFileUnderBothFlagsIsStillHashed covers an entry carrying assume-unchanged and
// skip-worktree at once, which git clears one command at a time.
func TestIntegrationAFileUnderBothFlagsIsStillHashed(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--assume-unchanged", "kept.go")
	run(t, root, "update-index", "--skip-worktree", "kept.go")
	write(t, root, "kept.go", "package main\n\nfunc Edited() {}\n")

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "kept.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main", "", "func Edited() {}"}, lines)
}

// TestIntegrationASkipWorktreeFileIsKeptBesideAnAssumeUnchangedOne is the sparse checkout again, in a
// worktree that also holds the flag a snapshot once seeded itself from the last commit over.
func TestIntegrationASkipWorktreeFileIsKeptBesideAnAssumeUnchangedOne(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--assume-unchanged", "proto/gen/api.pb.go")
	run(t, root, "update-index", "--skip-worktree", "kept.go")
	require.NoError(t, os.Remove(filepath.Join(root, "kept.go")))

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "kept.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main"}, lines)
}

// TestIntegrationTheIndexIsReadPastAnInheritedIndexFile covers a panel started with GIT_INDEX_FILE in its
// environment, which git would otherwise answer about instead of the agent's own index.
func TestIntegrationTheIndexIsReadPastAnInheritedIndexFile(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--skip-worktree", "kept.go")
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "elsewhere.index"))

	paths, err := hiddenPaths(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, []string{"kept.go"}, paths)
}

// TestIntegrationUnflaggingKeepsTheIndexTime covers git stamping an index it writes with the current time,
// which decides how much of the copy it is safe to trust a recorded stat for.
func TestIntegrationUnflaggingKeepsTheIndexTime(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--skip-worktree", "kept.go")

	index := filepath.Join(t.TempDir(), "round.index")
	env := append(append(os.Environ(), snapshotIdentity...), "GIT_INDEX_FILE="+index)
	require.NoError(t, copyIndex(t.Context(), root, index, env))

	before, err := os.Stat(index)
	require.NoError(t, err)

	require.NoError(t, revealPaths(t.Context(), root, index, env, []string{"kept.go"}))

	after, err := os.Stat(index)
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime())
}

// TestIntegrationASnapshotSurvivesARepositoryWithNoIndex covers a repository nothing has been added to yet,
// whose index file git has not written.
func TestIntegrationASnapshotSurvivesARepositoryWithNoIndex(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "--initial-branch=main")
	run(t, root, "config", "user.email", "test@example.com")
	run(t, root, "config", "user.name", "test")
	write(t, root, "loose.go", "package main\n")

	tree, err := client.ProbeTree(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, "loose.go", capture(t, root, "ls-tree", "--name-only", tree))
}

// TestIntegrationASnapshotSurvivesAnUnreadableIndex is the fallback: an index the agent's own git would
// refuse is no reason for the panel to refuse a diff.
func TestIntegrationASnapshotSurvivesAnUnreadableIndex(t *testing.T) {
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

// topicRound commits one change on a topic branch and takes a round of it.
func topicRound(t *testing.T, root string) *Snapshot {
	t.Helper()

	run(t, root, "checkout", "-b", "topic")
	write(t, root, "topic.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "topic")

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	return snapshot
}

// moveOn puts a commit of somebody else's on main, leaving the checkout where it was.
func moveOn(t *testing.T, root string) {
	t.Helper()

	was := capture(t, root, "rev-parse", "--abbrev-ref", "HEAD")
	run(t, root, "checkout", "main")
	write(t, root, "other-"+capture(t, root, "rev-parse", "--short", "HEAD")+".go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "somebody else's")
	run(t, root, "checkout", was)
}

// landOn squashes a round's content onto main the way a merge on the forge would, leaving the
// checkout on the branch it was on.
func landOn(t *testing.T, root, sha string) {
	t.Helper()

	was := capture(t, root, "rev-parse", "--abbrev-ref", "HEAD")
	run(t, root, "checkout", "main")
	run(t, root, "merge", "--squash", sha)
	run(t, root, "commit", "-m", "landed the round")
	run(t, root, "checkout", was)
}

// TestIntegrationHasLanded pins when the base branch counts as holding a round whole.
func TestIntegrationHasLanded(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		arrange func(t *testing.T, root string) *Snapshot
		want    bool
	}{
		{
			name: "a round that changed nothing never lands",
			base: "main",
			arrange: func(t *testing.T, root string) *Snapshot {
				run(t, root, "checkout", "-b", "topic")
				snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
				require.NoError(t, err)

				return snapshot
			},
			want: false,
		},
		{
			name:    "a base the clone does not have is not landed",
			base:    "nowhere",
			arrange: topicRound,
			want:    false,
		},
		{
			name:    "the worktree's own branch cannot hold the round",
			base:    "topic",
			arrange: topicRound,
			want:    false,
		},
		{
			name: "a base that has not taken the round",
			base: "main",
			arrange: func(t *testing.T, root string) *Snapshot {
				snapshot := topicRound(t, root)
				moveOn(t, root)

				return snapshot
			},
			want: false,
		},
		{
			name: "a base holding a later edit of the round",
			base: "main",
			arrange: func(t *testing.T, root string) *Snapshot {
				snapshot := topicRound(t, root)
				write(t, root, "topic.go", "package main\n\nfunc Changed() {}\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-m", "changed after the round")
				landOn(t, root, capture(t, root, "rev-parse", "HEAD"))

				return snapshot
			},
			want: false,
		},
		{
			name: "a round squashed onto the base",
			base: "main",
			arrange: func(t *testing.T, root string) *Snapshot {
				snapshot := topicRound(t, root)
				moveOn(t, root)
				landOn(t, root, snapshot.SHA)

				return snapshot
			},
			want: true,
		},
		{
			name: "an earlier round of work that later landed whole",
			base: "main",
			arrange: func(t *testing.T, root string) *Snapshot {
				snapshot := topicRound(t, root)
				write(t, root, "more.go", "package main\n")
				run(t, root, "add", "-A")
				run(t, root, "commit", "-m", "more")
				landOn(t, root, capture(t, root, "rev-parse", "HEAD"))

				return snapshot
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := repo(t)
			snapshot := tt.arrange(t, root)

			landed, err := client.HasLanded(t.Context(), HasLandedParams{
				Dir:         root,
				Base:        tt.base,
				BaseSHA:     snapshot.BaseSHA,
				SnapshotSHA: snapshot.SHA,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, landed)
		})
	}
}
