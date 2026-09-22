//go:build integration

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

// TestASkipWorktreeFileOnDiskIsStillHashed is the same invariant for the other flag git reads an
// index entry over a file for.
func TestASkipWorktreeFileOnDiskIsStillHashed(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--skip-worktree", "kept.go")
	write(t, root, "kept.go", "package main\n\nfunc Edited() {}\n")

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	lines, err := client.ReadFileLines(t.Context(), root, snapshot.SHA, "kept.go")
	require.NoError(t, err)
	assert.Equal(t, []string{"package main", "", "func Edited() {}"}, lines)
}

// TestAFileUnderBothFlagsIsStillHashed covers an entry carrying assume-unchanged and
// skip-worktree at once, which git clears one command at a time.
func TestAFileUnderBothFlagsIsStillHashed(t *testing.T) {
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

// TestASkipWorktreeFileIsKeptBesideAnAssumeUnchangedOne is the sparse checkout again, in a
// worktree that also holds the flag a snapshot once seeded itself from the last commit over.
func TestASkipWorktreeFileIsKeptBesideAnAssumeUnchangedOne(t *testing.T) {
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

// TestTheIndexIsReadPastAnInheritedIndexFile covers a panel started with GIT_INDEX_FILE in its
// environment, which git would otherwise answer about instead of the agent's own index.
func TestTheIndexIsReadPastAnInheritedIndexFile(t *testing.T) {
	root := repo(t)
	run(t, root, "update-index", "--skip-worktree", "kept.go")
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "elsewhere.index"))

	paths, err := hiddenPaths(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, []string{"kept.go"}, paths)
}

// TestUnflaggingKeepsTheIndexTime covers git stamping an index it writes with the current time,
// which decides how much of the copy it is safe to trust a recorded stat for.
func TestUnflaggingKeepsTheIndexTime(t *testing.T) {
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

// TestASnapshotSurvivesARepositoryWithNoIndex covers a repository nothing has been added to yet,
// whose index file git has not written.
func TestASnapshotSurvivesARepositoryWithNoIndex(t *testing.T) {
	root := t.TempDir()
	run(t, root, "init", "--initial-branch=main")
	run(t, root, "config", "user.email", "test@example.com")
	run(t, root, "config", "user.name", "test")
	write(t, root, "loose.go", "package main\n")

	tree, err := client.ProbeTree(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, "loose.go", capture(t, root, "ls-tree", "--name-only", tree))
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

// TestTheTreeStandsUntilTheWorktreeMoves is the comparison the review column rests on: the
// worktree a round was taken of hashes to that round until a file in it changes.
func TestTheTreeStandsUntilTheWorktreeMoves(t *testing.T) {
	root := repo(t)
	write(t, root, "loose.go", "package main\n")

	snapshot, err := client.TakeSnapshot(t.Context(), TakeSnapshotParams{Dir: root, Base: "main"})
	require.NoError(t, err)

	assert.Equal(t, StandingSame, standing(t, root, snapshot.SHA), "nothing has been written since the round was taken")

	write(t, root, "loose.go", "package main\n\nfunc Later() {}\n")

	assert.Equal(t, StandingMoved, standing(t, root, snapshot.SHA), "an edit to a file the round holds is a move")

	write(t, root, "loose.go", "package main\n")

	assert.Equal(t, StandingSame, standing(t, root, snapshot.SHA), "a worktree edited back to the round is not a move")
}

// TestARebaseMovesTheTree pins that a rebase bringing in a commit of the base's is a move until
// the base has taken the round, and that the agent's own commit riding on top is not new work.
func TestARebaseMovesTheTree(t *testing.T) {
	root := repo(t)
	snapshot := topicRound(t, root)

	run(t, root, "checkout", "main")
	write(t, root, "landed.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "landed")
	run(t, root, "checkout", "topic")

	assert.Equal(t, StandingSame, standing(t, root, snapshot.SHA), "the base moving on its own changes nothing in the agent's checkout")

	run(t, root, "rebase", "main")

	assert.Equal(t, StandingMoved, standing(t, root, snapshot.SHA), "the base does not hold the round, so the rebase is a move and nothing more")

	landOn(t, root, snapshot.SHA)

	assert.Equal(t, StandingLanded, standing(t, root, snapshot.SHA), "the commit on top is the one the base took, so nothing on disk is unread")

	write(t, root, "later.go", "package main\n")

	assert.Equal(t, StandingLandedMoved, standing(t, root, snapshot.SHA))
}

// TestAnUnreadableBaseIsRefused pins that a base git cannot find is reported rather than read
// as a standing against something else.
func TestAnUnreadableBaseIsRefused(t *testing.T) {
	root := repo(t)
	snapshot := topicRound(t, root)
	write(t, root, "later.go", "package main\n")

	_, err := client.ReadStanding(t.Context(), ReadStandingParams{
		Dir:         root,
		Base:        "develop",
		SnapshotSHA: snapshot.SHA,
	})
	require.ErrorIs(t, err, ErrBaseMissing)

	for _, base := range []string{"", "HEAD"} {
		_, err := client.ReadStanding(t.Context(), ReadStandingParams{
			Dir:         root,
			Base:        base,
			SnapshotSHA: snapshot.SHA,
		})
		require.ErrorIs(t, err, ErrBaseMissing, "base %q", base)
	}
}

// TestABaseThatIsTheHeadCannotHoldTheRound pins that a base naming the branch the agent is on
// never reads as having taken the round.
func TestABaseThatIsTheHeadCannotHoldTheRound(t *testing.T) {
	root := repo(t)
	snapshot := topicRound(t, root)
	write(t, root, "later.go", "package main\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "later")

	standing, err := client.ReadStanding(t.Context(), ReadStandingParams{
		Dir:         root,
		Base:        "topic",
		SnapshotSHA: snapshot.SHA,
	})
	require.NoError(t, err)
	assert.Equal(t, StandingMoved, standing)
}

// TestALandedRoundWithNothingOnTopIsLanded is the round shipped and the branch reset onto the
// base that took it.
func TestALandedRoundWithNothingOnTopIsLanded(t *testing.T) {
	root := repo(t)
	snapshot := topicRound(t, root)
	moveOn(t, root)
	landOn(t, root, snapshot.SHA)

	run(t, root, "reset", "--hard", "main")

	assert.Equal(t, StandingLanded, standing(t, root, snapshot.SHA))
}

// TestALandedRoundWithNewWorkOnTopIsLandedAndMoved is the round shipped and the agent writing
// more since.
func TestALandedRoundWithNewWorkOnTopIsLandedAndMoved(t *testing.T) {
	root := repo(t)
	snapshot := topicRound(t, root)
	moveOn(t, root)
	landOn(t, root, snapshot.SHA)

	run(t, root, "reset", "--hard", "main")
	write(t, root, "later.go", "package main\n")

	assert.Equal(t, StandingLandedMoved, standing(t, root, snapshot.SHA), "an uncommitted file is new work as much as a commit is")

	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "later")

	assert.Equal(t, StandingLandedMoved, standing(t, root, snapshot.SHA))
}

// TestABranchBehindTheBaseTipIsStillClean pins that an agent sitting on an older base with
// nothing of its own is clean however far the base has moved on.
func TestABranchBehindTheBaseTipIsStillClean(t *testing.T) {
	root := repo(t)
	snapshot := topicRound(t, root)
	moveOn(t, root)
	landOn(t, root, snapshot.SHA)
	run(t, root, "reset", "--hard", "main")
	moveOn(t, root)

	assert.Equal(t, StandingLanded, standing(t, root, snapshot.SHA))
}

// TestAStaleBaseReadsAsMovedNeverAsLanded pins that a base nobody has fetched errs toward a
// pass owed and never toward green.
func TestAStaleBaseReadsAsMovedNeverAsLanded(t *testing.T) {
	root := repo(t)
	snapshot := topicRound(t, root)
	moveOn(t, root)
	landOn(t, root, snapshot.SHA)
	run(t, root, "reset", "--hard", "main")

	run(t, root, "branch", "-f", "main", "main~1")

	assert.Equal(t, StandingMoved, standing(t, root, snapshot.SHA))
}

// TestAnEditedRoundThatLandedIsNotLanded pins that landed means the base took the round itself
// rather than a later state of the same branch.
func TestAnEditedRoundThatLandedIsNotLanded(t *testing.T) {
	root := repo(t)
	snapshot := topicRound(t, root)

	write(t, root, "topic.go", "package main\n\nfunc Changed() {}\n")
	run(t, root, "add", "-A")
	run(t, root, "commit", "-m", "changed after the round")
	run(t, root, "checkout", "main")
	run(t, root, "merge", "--ff-only", "topic")
	run(t, root, "checkout", "topic")

	assert.Equal(t, StandingMoved, standing(t, root, snapshot.SHA), "the base holds something other than the round that was approved")
}

func standing(t *testing.T, root, sha string) Standing {
	t.Helper()

	standing, err := client.ReadStanding(t.Context(), ReadStandingParams{
		Dir:         root,
		Base:        "main",
		SnapshotSHA: sha,
	})
	require.NoError(t, err)

	return standing
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
