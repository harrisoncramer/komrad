//go:build integration

package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// client is the git these tests drive.
var client = git.NewClient()

// TestReadTreeStateIgnoresUntrackedFiles pins the fact the dirty refusal turns on.
func TestReadTreeStateIgnoresUntrackedFiles(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	writeFile(t, repo, "scratch.txt", "untracked")

	state, err := client.ReadTreeState(t.Context(), repo)
	require.NoError(t, err)

	assert.False(t, state.TrackedChanges, "an untracked file is not a tracked change")
	assert.False(t, state.InProgress)
	assert.Equal(t, "main", state.Branch)
}

// TestReadTreeStateSeesTrackedChanges covers the half that does block a rebase, staged or not.
func TestReadTreeStateSeesTrackedChanges(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		stage bool
	}{
		{name: "unstaged"},
		{name: "staged", stage: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := newRepo(t)
			writeFile(t, repo, "tracked.txt", "edited")
			if tt.stage {
				run(t, repo, "add", "tracked.txt")
			}

			state, err := client.ReadTreeState(t.Context(), repo)
			require.NoError(t, err)

			assert.True(t, state.TrackedChanges)
		})
	}
}

// TestReadTreeStateReportsADetachedHead covers the case where a rebase reports success while
// leaving the branch exactly where it was, so it has to be refused rather than run.
func TestReadTreeStateReportsADetachedHead(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "--detach", "HEAD")

	state, err := client.ReadTreeState(t.Context(), repo)
	require.NoError(t, err)

	assert.Empty(t, state.Branch)
}

// TestReadTreeStateReportsAStoppedSequence covers a checkout already part-way through
// something that owns the index, which a rebase cannot start over.
func TestReadTreeStateReportsAStoppedSequence(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	conflict(t, repo)

	state, err := client.ReadTreeState(t.Context(), repo)
	require.NoError(t, err)

	assert.True(t, state.InProgress)
}

// TestRebaseMovesTheBranchOnto is the operation working, with the untracked file surviving it.
func TestRebaseMovesTheBranchOnto(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	run(t, repo, "commit", "--quiet", "--allow-empty", "-m", "the agent's work")
	run(t, repo, "checkout", "--quiet", "main")
	run(t, repo, "commit", "--quiet", "--allow-empty", "-m", "main moved on")
	base := out(t, repo, "rev-parse", "HEAD")
	run(t, repo, "checkout", "--quiet", "agent/abc")
	writeFile(t, repo, "scratch.txt", "untracked")

	count, err := client.CountCommitsToReplay(t.Context(), repo, "main")
	require.NoError(t, err)
	assert.Equal(t, 1, count, "one commit of the agent's own to replay")

	require.NoError(t, client.Rebase(t.Context(), repo, "main"))

	assert.Equal(t, base, out(t, repo, "rev-parse", "HEAD~1"), "the branch now sits on the base")
	body, err := os.ReadFile(filepath.Join(repo, "scratch.txt"))
	require.NoError(t, err)
	assert.Equal(t, "untracked", string(body), "the untracked file rode through the rebase")
}

// TestRebaseLeavesAConflictStopped pins that a conflicted rebase is reported and left where it is.
func TestRebaseLeavesAConflictStopped(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	writeFile(t, repo, "clash.txt", "theirs\n")
	run(t, repo, "add", "clash.txt")
	run(t, repo, "commit", "--quiet", "-m", "theirs")
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc", "HEAD~1")
	writeFile(t, repo, "clash.txt", "ours\n")
	run(t, repo, "add", "clash.txt")
	run(t, repo, "commit", "--quiet", "-m", "ours")

	err := client.Rebase(t.Context(), repo, "main")
	require.ErrorIs(t, err, git.ErrRebaseConflicted)

	state, stateErr := client.ReadTreeState(t.Context(), repo)
	require.NoError(t, stateErr)
	assert.True(t, state.InProgress, "the stopped rebase is still there for the agent to settle")
	assert.Empty(t, state.Branch, "a stopped rebase detaches the head, which blanks the branch")
}

// TestFetchBaseRefusesARepositoryWithNoOrigin pins the fetch failing before the base resolves.
func TestFetchBaseRefusesARepositoryWithNoOrigin(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)

	_, _, err := client.FetchBase(t.Context(), repo, "main")
	require.Error(t, err)
}

// TestFetchBaseRefusesABaseOriginDoesNotHave covers a workflow naming a branch origin no longer
// has, which is reported as that rather than measured against something else.
func TestFetchBaseRefusesABaseOriginDoesNotHave(t *testing.T) {
	t.Parallel()

	_, clone := newClonePair(t)

	_, _, err := client.FetchBase(t.Context(), clone, "trunk")
	require.Error(t, err)
	require.NotErrorIs(t, err, git.ErrBaseMissing, "a branch origin has never had fails at the fetch")

	_, _, err = client.FetchBase(t.Context(), clone, "")
	require.ErrorIs(t, err, git.ErrBaseMissing, "a workflow naming no base is refused before anything is fetched")
}

// TestFetchBaseFetchesTheNamedBase pins the base being fetched by name rather than read from
// the clone's cache.
func TestFetchBaseFetchesTheNamedBase(t *testing.T) {
	t.Parallel()

	origin, clone := newClonePair(t)
	run(t, clone, "symbolic-ref", "-d", "refs/remotes/origin/HEAD")

	// The origin moves on after the clone, so a stale remote-tracking ref and a fetched one
	// name different commits.
	run(t, origin, "commit", "--quiet", "--allow-empty", "-m", "origin moved on")
	moved := out(t, origin, "rev-parse", "HEAD")

	ref, sha, err := client.FetchBase(t.Context(), clone, "main")
	require.NoError(t, err)

	assert.Equal(t, "origin/main", ref, "the base resolves to a remote-tracking ref without origin/HEAD")
	assert.Equal(t, moved, sha, "the base was fetched rather than read from the clone's cache")
}

// TestReadTreeStateRefreshesTheIndex covers a file rewritten with identical content, whose
// stale stat data makes diff-index read a clean tree as changed.
func TestReadTreeStateRefreshesTheIndex(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)

	body, err := os.ReadFile(filepath.Join(repo, "tracked.txt"))
	require.NoError(t, err)
	writeFile(t, repo, "tracked.txt", string(body))
	require.NoError(t, os.Chtimes(filepath.Join(repo, "tracked.txt"), time.Time{}, time.Unix(1, 0)))

	state, err := client.ReadTreeState(t.Context(), repo)
	require.NoError(t, err)

	assert.False(t, state.TrackedChanges, "identical content with a stale mtime is not a change")
}

// newClonePair is an origin and a clone of it, so a fetch has somewhere to fetch from.
func newClonePair(t *testing.T) (origin, clone string) {
	t.Helper()

	origin = newRepo(t)
	clone = filepath.Join(t.TempDir(), "clone")
	run(t, filepath.Dir(clone), "clone", "--quiet", origin, clone)
	run(t, clone, "config", "user.email", "t@example.com")
	run(t, clone, "config", "user.name", "t")

	return origin, clone
}

func newRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	run(t, dir, "init", "--quiet", "-b", "main", ".")
	run(t, dir, "config", "user.email", "t@example.com")
	run(t, dir, "config", "user.name", "t")
	writeFile(t, dir, "tracked.txt", "base")
	run(t, dir, "add", "tracked.txt")
	run(t, dir, "commit", "--quiet", "-m", "base")

	return dir
}

// conflict leaves a checkout part-way through a merge it cannot settle on its own.
func conflict(t *testing.T, dir string) {
	t.Helper()

	writeFile(t, dir, "clash.txt", "ours\n")
	run(t, dir, "add", "clash.txt")
	run(t, dir, "commit", "--quiet", "-m", "ours")
	run(t, dir, "checkout", "--quiet", "-b", "theirs", "HEAD~1")
	writeFile(t, dir, "clash.txt", "theirs\n")
	run(t, dir, "add", "clash.txt")
	run(t, dir, "commit", "--quiet", "-m", "theirs")
	run(t, dir, "checkout", "--quiet", "-")

	// Expected to fail.
	_, _ = git.Run(t.Context(), dir, "merge", "theirs")

	state, err := client.ReadTreeState(t.Context(), dir)
	require.NoError(t, err)
	require.True(t, state.InProgress, "failed to build a stopped sequence")
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()

	_, err := git.Run(t.Context(), dir, args...)
	require.NoError(t, err, "git %v", args)
}

func out(t *testing.T, dir string, args ...string) string {
	t.Helper()

	text, err := git.Run(t.Context(), dir, args...)
	require.NoError(t, err, "git %v", args)

	return text
}

// squashLanded lands everything the branch has on main as one squashed commit.
func squashLanded(t *testing.T, repo, message string) {
	t.Helper()

	branch := out(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	run(t, repo, "checkout", "--quiet", "main")
	run(t, repo, "merge", "--quiet", "--squash", branch)
	run(t, repo, "commit", "--quiet", "-m", message)
	run(t, repo, "checkout", "--quiet", branch)
}

// moveMain lands an unrelated commit on main, standing for another pull request merging while
// this branch was busy.
func moveMain(t *testing.T, repo, name, body string) {
	t.Helper()

	branch := out(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	run(t, repo, "checkout", "--quiet", "main")
	writeFile(t, repo, name, body)
	run(t, repo, "add", name)
	run(t, repo, "commit", "--quiet", "-m", "another pull request")
	run(t, repo, "checkout", "--quiet", branch)
}

// commitFile is one commit touching one path.
func commitFile(t *testing.T, repo, name, body, message string) string {
	t.Helper()

	writeFile(t, repo, name, body)
	run(t, repo, "add", name)
	run(t, repo, "commit", "--quiet", "-m", message)

	return out(t, repo, "rev-parse", "HEAD")
}

// commitFileAt is commitFile with the commit's date pinned.
func commitFileAt(t *testing.T, repo, name, body, message, when string) string {
	t.Helper()

	writeFile(t, repo, name, body)
	run(t, repo, "add", name)

	cmd := exec.Command("git", "-C", repo, "commit", "--quiet", "-m", message)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when)
	combined, err := cmd.CombinedOutput()
	require.NoError(t, err, string(combined))

	return out(t, repo, "rev-parse", "HEAD")
}

// TestPredictRebaseFindsTheCutoffOfASquashedBranch is the case this exists for, with the base
// moved on by other work so the trees differ and only the content walk can see it.
func TestPredictRebaseFindsTheCutoffOfASquashedBranch(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "f.txt", "first draft\n", "A")
	landed := commitFile(t, repo, "f.txt", "second draft\n", "B")
	squashLanded(t, repo, "A and B squashed (#1)")
	moveMain(t, repo, "elsewhere.txt", "elsewhere\n")
	commitFile(t, repo, "g.txt", "new work\n", "C")

	require.NotEqual(t, out(t, repo, "rev-parse", "main^{tree}"), out(t, repo, "rev-parse", "HEAD^{tree}"),
		"the fixture must have differing trees, or it is not the case this is about")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)

	assert.Equal(t, landed, prediction.Cutoff, "the cutoff is the tip that landed")
	assert.Equal(t, 2, prediction.Landed)
	assert.Equal(t, 1, prediction.Replay, "only the commit written after the merge is replayed")
}

// TestPredictRebaseCutoffIsTheTipWhenEverythingLanded covers the branch that wrote nothing after
// its merge, where replaying nothing leaves it standing on the base.
func TestPredictRebaseCutoffIsTheTipWhenEverythingLanded(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "f.txt", "first\n", "A")

	// B is landed too, and reads as landed on its own, so a walk that answered with the oldest
	// landed commit rather than the newest would stop here and replay C for nothing.
	middle := commitFile(t, repo, "f.txt", "second\n", "B")
	tip := commitFile(t, repo, "g.txt", "more\n", "C")
	squashLanded(t, repo, "squashed (#1)")
	moveMain(t, repo, "elsewhere.txt", "elsewhere\n")

	base := out(t, repo, "rev-parse", "main^{tree}")
	middleMerged, err := git.Run(t.Context(), repo, "merge-tree", "--write-tree", "main", middle)
	require.NoError(t, err)
	require.Equal(t, base, middleMerged, "the fixture needs a landed commit behind the tip to be about anything")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)

	assert.Equal(t, tip, prediction.Cutoff, "the newest landed commit, not the oldest")
	assert.Equal(t, 3, prediction.Landed)
	assert.Zero(t, prediction.Replay)
}

// TestPredictRebaseFindsNoCutoffOnAnOrdinaryBranch pins the ordinary case finding nothing.
func TestPredictRebaseFindsNoCutoffOnAnOrdinaryBranch(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "mine.txt", "mine\n", "mine")
	moveMain(t, repo, "theirs.txt", "theirs\n")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)

	assert.Empty(t, prediction.Cutoff)
	assert.Zero(t, prediction.Landed)
	assert.Equal(t, 1, prediction.Replay)
}

// TestPredictRebaseWalksRatherThanHalves pins the property that decides the implementation: a
// commit inside a landed run reads as not landed, so the predicate cannot be binary searched.
func TestPredictRebaseWalksRatherThanHalves(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	first := commitFile(t, repo, "f.txt", "first draft\n", "A")
	second := commitFile(t, repo, "f.txt", "second draft\n", "B")
	squashLanded(t, repo, "squashed (#1)")

	base := out(t, repo, "rev-parse", "main^{tree}")

	_, firstErr := git.Run(t.Context(), repo, "merge-tree", "--write-tree", "main", first)
	require.Error(t, firstErr, "the intermediate commit holds content the base never took")

	secondMerged, secondErr := git.Run(t.Context(), repo, "merge-tree", "--write-tree", "main", second)
	require.NoError(t, secondErr)
	assert.Equal(t, base, secondMerged, "while the tip of the landed run reads as landed")
}

// TestPredictRebaseRefusesABaseTheBranchAlreadyContains covers a branch that committed something
// and then reverted it, which holds the base's own tree with no merge having happened.
func TestPredictRebaseRefusesABaseTheBranchAlreadyContains(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "spike.txt", "an experiment\n", "try it")
	run(t, repo, "rm", "--quiet", "spike.txt")
	run(t, repo, "commit", "--quiet", "-m", "back it out")

	require.Equal(t, out(t, repo, "rev-parse", "main^{tree}"), out(t, repo, "rev-parse", "HEAD^{tree}"),
		"the fixture did not produce the equal trees it is about")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)

	assert.Empty(t, prediction.Cutoff, "the base is already contained, so nothing is dropped")
	assert.Equal(t, 2, prediction.Replay)
}

// TestPredictRebaseRefusesAnUnrelatedBase covers a base sharing no history, where every question
// about what the two have in common answers wrongly rather than failing.
func TestPredictRebaseRefusesAnUnrelatedBase(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "--orphan", "agent/abc")
	run(t, repo, "rm", "-rf", "--quiet", ".")
	commitFile(t, repo, "alone.txt", "alone\n", "unrelated root")

	_, err := client.PredictRebase(t.Context(), repo, "main")
	require.ErrorIs(t, err, git.ErrBaseUnrelated)
}

// TestReadTreeStateReportsAShallowClone covers a clone cut off from its own history, read
// through a linked worktree because that is the only shape an agent ever runs in.
func TestReadTreeStateReportsAShallowClone(t *testing.T) {
	t.Parallel()

	origin := newRepo(t)
	run(t, origin, "commit", "--quiet", "--allow-empty", "-m", "second")
	run(t, origin, "commit", "--quiet", "--allow-empty", "-m", "third")

	root := t.TempDir()
	clone := filepath.Join(root, "clone")
	run(t, root, "clone", "--quiet", "--depth", "1", "file://"+origin, clone)

	tree := filepath.Join(root, "tree")
	run(t, clone, "worktree", "add", "--quiet", "-b", "agent/abc", tree)

	require.NoFileExists(t, filepath.Join(clone, ".git", "worktrees", "tree", "shallow"),
		"the marker is not in the worktree's own git directory, which is what makes this test worth having")

	state, err := client.ReadTreeState(t.Context(), tree)
	require.NoError(t, err)

	assert.True(t, state.Shallow)
}

// TestReadTreeStateNamesPathsTheIndexIsHolding covers a path marked assume-unchanged, whose
// working copy nothing can read out and so nothing can put back.
func TestReadTreeStateNamesPathsTheIndexIsHolding(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "update-index", "--assume-unchanged", "tracked.txt")

	state, err := client.ReadTreeState(t.Context(), repo)
	require.NoError(t, err)

	assert.Equal(t, []string{"tracked.txt"}, state.HiddenPaths)
}

// TestRebaseOntoReplaysOnlyWhatFollowsTheCutoff is the operation the cutoff exists to feed,
// against the branch a plain rebase cannot settle.
func TestRebaseOntoReplaysOnlyWhatFollowsTheCutoff(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "f.txt", "first draft\n", "A")
	commitFile(t, repo, "f.txt", "second draft\n", "B")
	squashLanded(t, repo, "squashed (#1)")
	moveMain(t, repo, "elsewhere.txt", "elsewhere\n")
	commitFile(t, repo, "g.txt", "new work\n", "C")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)
	require.NotEmpty(t, prediction.Cutoff, "the fixture did not produce the cutoff it is about")

	require.Error(t, client.Rebase(t.Context(), repo, "main"), "a plain rebase cannot settle this branch")
	run(t, repo, "rebase", "--abort")

	require.NoError(t, client.RebaseOnto(t.Context(), repo, "main", prediction.Cutoff))

	assert.Equal(t, "new work\n", readFile(t, repo, "g.txt"), "the work written after the merge survived")
	assert.Equal(t, "elsewhere\n", readFile(t, repo, "elsewhere.txt"), "and the other pull request came in")
	assert.Equal(t, "1", out(t, repo, "rev-list", "--count", "main..HEAD"), "one commit of its own remains")
}

// TestRebaseOntoRefusesWhatARebaseRefuses pins the guard a plain rebase has and reset does not,
// which is what makes replaying safe against an agent still writing.
func TestRebaseOntoRefusesWhatARebaseRefuses(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "f.txt", "first\n", "A")
	squashLanded(t, repo, "squashed (#1)")
	moveMain(t, repo, "elsewhere.txt", "elsewhere\n")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)
	require.NotEmpty(t, prediction.Cutoff)

	writeFile(t, repo, "tracked.txt", "written while the operation was deciding")

	require.Error(t, client.RebaseOnto(t.Context(), repo, "main", prediction.Cutoff))
	assert.Equal(t, "written while the operation was deciding", readFile(t, repo, "tracked.txt"),
		"the edit is still there rather than reverted")
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)

	return string(body)
}

// TestParkRecoversWhatAMoveDrops covers the two things nothing else recovers: the commits a
// replay from a cutoff drops on purpose, and the files that were never committed at all.
func TestParkRecoversWhatAMoveDrops(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "f.txt", "first draft\n", "A")
	commitFile(t, repo, "f.txt", "second draft\n", "B")
	squashLanded(t, repo, "squashed (#1)")
	moveMain(t, repo, "elsewhere.txt", "elsewhere\n")
	writeFile(t, repo, "scratch.txt", "never committed")

	dropped := out(t, repo, "rev-parse", "HEAD")
	parked, err := client.Park(t.Context(), repo, "refs/weave/prerefresh/a1/1")
	require.NoError(t, err)

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)
	require.NotEmpty(t, prediction.Cutoff)
	require.NoError(t, client.RebaseOnto(t.Context(), repo, "main", prediction.Cutoff))

	require.Equal(t, "0", out(t, repo, "rev-list", "--count", "main..HEAD"),
		"the fixture did not drop the commits this is about")

	assert.Equal(t, dropped, out(t, repo, "rev-parse", parked+"^"),
		"the commits the move dropped are still reachable from the park")
	assert.Equal(t, "never committed", out(t, repo, "show", parked+":scratch.txt"),
		"and so is a file that was never committed")
}

// TestParkLeavesTheCheckoutAlone pins the park being free of the agent's own state.
func TestParkLeavesTheCheckoutAlone(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	writeFile(t, repo, "scratch.txt", "untracked")
	writeFile(t, repo, "tracked.txt", "edited")
	before := out(t, repo, "status", "--porcelain")
	head := out(t, repo, "rev-parse", "HEAD")

	_, err := client.Park(t.Context(), repo, "refs/weave/prerefresh/a1/1")
	require.NoError(t, err)

	assert.Equal(t, before, out(t, repo, "status", "--porcelain"), "the working tree reads the same")
	assert.Equal(t, head, out(t, repo, "rev-parse", "HEAD"), "and the branch has not moved")
}

// TestReleaseRefsUnderKeepsOnlyTheCurrent covers the parks being collected.
func TestReleaseRefsUnderKeepsOnlyTheCurrent(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	head := out(t, repo, "rev-parse", "HEAD")
	for _, ref := range []string{"refs/weave/prerefresh/a1/1", "refs/weave/prerefresh/a1/2", "refs/weave/prerefresh/a2/1"} {
		run(t, repo, "update-ref", ref, head)
	}

	require.NoError(t, client.ReleaseRefsUnder(t.Context(), repo, "refs/weave/prerefresh/a1/", "refs/weave/prerefresh/a1/2"))

	left := out(t, repo, "for-each-ref", "--format=%(refname)", "refs/weave/prerefresh/")
	assert.Equal(t, "refs/weave/prerefresh/a1/2\nrefs/weave/prerefresh/a2/1", left,
		"the current park stays and another agent's is untouched")
}

// TestPredictRebaseCountsWhatARebaseWouldReplay covers merge commits, which a rebase discards
// rather than replays, so counting positions in the commit list overstates the work.
func TestPredictRebaseCountsWhatARebaseWouldReplay(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "mine.txt", "mine\n", "mine")
	moveMain(t, repo, "theirs.txt", "theirs\n")
	run(t, repo, "merge", "--quiet", "--no-ff", "-m", "merge main", "main")

	require.Equal(t, "2", out(t, repo, "rev-list", "--count", "main..HEAD"),
		"two commits of its own, one of them the merge")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)

	assert.Equal(t, 1, prediction.Merges)
	assert.Equal(t, 1, prediction.Replay, "the merge is discarded rather than replayed")
}

// TestPredictRebaseCountsLandedOffTheGraph covers a side branch merged in, which sits before the
// cutoff in topological order while not being reachable from it.
func TestPredictRebaseCountsLandedOffTheGraph(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	base := out(t, repo, "rev-parse", "HEAD")
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	landed := commitFileAt(t, repo, "a.txt", "a\n", "A", "2020-01-01T00:00:09+00:00")
	run(t, repo, "checkout", "--quiet", "-b", "side", base)
	commitFileAt(t, repo, "s.txt", "s\n", "S", "2020-01-01T00:00:01+00:00")
	run(t, repo, "checkout", "--quiet", "agent/abc")
	run(t, repo, "merge", "--quiet", "--no-ff", "-m", "M", "side")

	// The base gains A's content on its own, the way a squash merge puts it there.
	run(t, repo, "checkout", "--quiet", "main")
	writeFile(t, repo, "a.txt", "a\n")
	run(t, repo, "add", "a.txt")
	run(t, repo, "commit", "--quiet", "-m", "A, landed independently")
	run(t, repo, "checkout", "--quiet", "agent/abc")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)

	require.Equal(t, landed, prediction.Cutoff, "the cutoff is the commit the base holds")
	listed := out(t, repo, "rev-list", "--reverse", "main..HEAD")
	require.NotEqual(t, prediction.Cutoff, strings.SplitN(listed, "\n", 2)[0],
		"the fixture needs the side commit listed before the cutoff, or it is not about anything")
	assert.Equal(t, 1, prediction.Landed, "only the commit the base actually holds leaves the branch")
	assert.Equal(t, 1, prediction.Replay, "the side commit is still the agent's own work")
}

// TestPredictRebaseCountsMergesAsLanded pins the landed count including merge commits, unlike the
// replay count.
func TestPredictRebaseCountsMergesAsLanded(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	base := out(t, repo, "rev-parse", "HEAD")
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "s.txt", "s\n", "S")
	run(t, repo, "checkout", "--quiet", "-b", "side", base)
	commitFile(t, repo, "t.txt", "t\n", "T")
	run(t, repo, "checkout", "--quiet", "agent/abc")
	run(t, repo, "merge", "--quiet", "--no-ff", "-m", "M", "side")

	// The base gains both files on its own, so the whole branch including its merge has landed.
	run(t, repo, "checkout", "--quiet", "main")
	writeFile(t, repo, "s.txt", "s\n")
	writeFile(t, repo, "t.txt", "t\n")
	run(t, repo, "add", "s.txt", "t.txt")
	run(t, repo, "commit", "--quiet", "-m", "both, landed independently")
	run(t, repo, "checkout", "--quiet", "agent/abc")

	require.Equal(t, "1", out(t, repo, "rev-list", "--count", "--merges", "main..HEAD"),
		"the fixture needs a merge inside the landed run, or it is not about anything")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)

	require.Zero(t, prediction.Replay, "the whole branch landed")
	assert.Equal(t, 3, prediction.Landed, "the merge counts among the commits leaving the branch")
}

// TestPredictRebaseCountsOnlyWhatWouldReplay covers a commit past the cutoff whose patch the
// base already holds, which git skips, so counting the commits after the cutoff overstates it.
func TestPredictRebaseCountsOnlyWhatWouldReplay(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	run(t, repo, "checkout", "--quiet", "-b", "agent/abc")
	commitFile(t, repo, "a.txt", "a\n", "A")
	commitFile(t, repo, "b.txt", "b\n", "B")
	picked := commitFile(t, repo, "c.txt", "c\n", "C")

	// A reaches the base as a squash and C as a cherry-pick, leaving B behind between them.
	run(t, repo, "checkout", "--quiet", "main")
	writeFile(t, repo, "a.txt", "a\n")
	run(t, repo, "add", "a.txt")
	run(t, repo, "commit", "--quiet", "-m", "A squashed")
	run(t, repo, "cherry-pick", picked)
	run(t, repo, "checkout", "--quiet", "agent/abc")

	prediction, err := client.PredictRebase(t.Context(), repo, "main")
	require.NoError(t, err)
	require.Equal(t, "2", out(t, repo, "rev-list", "--count", prediction.Cutoff+"..HEAD"),
		"two commits sit past the cutoff, which is what makes the naive count wrong")

	assert.Equal(t, 1, prediction.Replay, "only one of them still owes the base anything")

	require.NoError(t, client.RebaseOnto(t.Context(), repo, "main", prediction.Cutoff))
	assert.Equal(t, "1", out(t, repo, "rev-list", "--count", "main..HEAD"),
		"and that is what git actually replayed")
}
