package git_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadTreeStateIgnoresUntrackedFiles pins the fact the dirty refusal turns on.
func TestReadTreeStateIgnoresUntrackedFiles(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	writeFile(t, repo, "scratch.txt", "untracked")

	state, err := git.ReadTreeState(t.Context(), repo)
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

			state, err := git.ReadTreeState(t.Context(), repo)
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

	state, err := git.ReadTreeState(t.Context(), repo)
	require.NoError(t, err)

	assert.Empty(t, state.Branch)
}

// TestReadTreeStateReportsAStoppedSequence covers a checkout already part-way through
// something that owns the index, which a rebase cannot start over.
func TestReadTreeStateReportsAStoppedSequence(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)
	conflict(t, repo)

	state, err := git.ReadTreeState(t.Context(), repo)
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

	count, err := git.CountCommitsToReplay(t.Context(), repo, "main")
	require.NoError(t, err)
	assert.Equal(t, 1, count, "one commit of the agent's own to replay")

	require.NoError(t, git.Rebase(t.Context(), repo, "main"))

	assert.Equal(t, base, out(t, repo, "rev-parse", "HEAD~1"), "the branch now sits on the base")
	body, err := os.ReadFile(filepath.Join(repo, "scratch.txt"))
	require.NoError(t, err)
	assert.Equal(t, "untracked", string(body), "the untracked file rode through the rebase")
}

// TestRebaseLeavesAConflictStopped pins that a conflicted rebase is reported and left where it
// is, rather than aborted on the agent's behalf.
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

	err := git.Rebase(t.Context(), repo, "main")
	require.ErrorIs(t, err, git.ErrRebaseConflicted)

	state, stateErr := git.ReadTreeState(t.Context(), repo)
	require.NoError(t, stateErr)
	assert.True(t, state.InProgress, "the stopped rebase is still there for the agent to settle")
	assert.Empty(t, state.Branch, "a stopped rebase detaches the head, which blanks the branch")
}

// TestFetchBaseRefusesARepositoryWithNoOrigin pins the fetch failing before the base resolves.
func TestFetchBaseRefusesARepositoryWithNoOrigin(t *testing.T) {
	t.Parallel()

	repo := newRepo(t)

	_, _, err := git.FetchBase(t.Context(), repo, "")
	require.Error(t, err)
}

// TestFetchBaseRefusesABaseThatResolvesToHead covers a blank base resolving to the literal
// HEAD, which a rebase would succeed against trivially and report as a success.
func TestFetchBaseRefusesABaseThatResolvesToHead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	run(t, root, "init", "--quiet", "--bare", "-b", "main", bare)

	clone := filepath.Join(root, "clone")
	run(t, root, "clone", "--quiet", bare, clone)

	_, _, err := git.FetchBase(t.Context(), clone, "")
	require.ErrorIs(t, err, git.ErrBaseUnresolved)
}

// TestFetchBaseRepairsAMissingOriginHead pins why the guard above needs an empty remote to reach.
func TestFetchBaseRepairsAMissingOriginHead(t *testing.T) {
	t.Parallel()

	_, clone := newClonePair(t)
	run(t, clone, "symbolic-ref", "-d", "refs/remotes/origin/HEAD")

	ref, _, err := git.FetchBase(t.Context(), clone, "")
	require.NoError(t, err)

	assert.Equal(t, "origin/main", ref, "the unqualified fetch put origin/HEAD back")
}

// TestFetchBaseFetchesWhenNoBaseIsNamed pins the default configuration working, since the
// workflow form leaves the base branch blank for most workflows.
func TestFetchBaseFetchesWhenNoBaseIsNamed(t *testing.T) {
	t.Parallel()

	origin, clone := newClonePair(t)

	// The origin moves on after the clone, so a stale remote-tracking ref and a fetched one
	// name different commits.
	run(t, origin, "commit", "--quiet", "--allow-empty", "-m", "origin moved on")
	moved := out(t, origin, "rev-parse", "HEAD")

	ref, sha, err := git.FetchBase(t.Context(), clone, "")
	require.NoError(t, err)

	assert.Equal(t, "origin/main", ref, "the base resolves to a remote-tracking ref, never to HEAD")
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

	state, err := git.ReadTreeState(t.Context(), repo)
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

	// Expected to fail, which is the point.
	_, _ = git.Run(t.Context(), dir, "merge", "theirs")

	state, err := git.ReadTreeState(t.Context(), dir)
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
