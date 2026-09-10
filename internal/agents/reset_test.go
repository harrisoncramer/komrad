package agents

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildResetWorktree is an agent worktree with work of every kind in it: a commit of its own,
// an uncommitted edit, an untracked file and an ignored one.
func buildResetWorktree(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	gitRun(t, root, "init", "--quiet", "-b", "main", origin)
	gitRun(t, origin, "config", "user.email", "t@example.com")
	gitRun(t, origin, "config", "user.name", "t")
	writeTree(t, origin, "tracked.txt", "base\n")
	writeTree(t, origin, ".gitignore", "secrets.env\n")
	gitRun(t, origin, "add", "-A")
	gitRun(t, origin, "commit", "--quiet", "-m", "base")

	clone := filepath.Join(root, "clone")
	gitRun(t, root, "clone", "--quiet", origin, clone)
	gitRun(t, clone, "config", "user.email", "t@example.com")
	gitRun(t, clone, "config", "user.name", "t")

	tree := filepath.Join(root, "tree")
	gitRun(t, clone, "worktree", "add", "--quiet", "--no-track", "-b", "agent/a1", tree, "origin/main")

	writeTree(t, tree, "attempt.txt", "a bad attempt\n")
	gitRun(t, tree, "add", "attempt.txt")
	gitRun(t, tree, "commit", "--quiet", "-m", "an attempt worth abandoning")
	writeTree(t, tree, "tracked.txt", "edited and not committed\n")
	writeTree(t, tree, "scratch.txt", "untracked\n")
	writeTree(t, tree, "secrets.env", "SECRET=hunter2\n")

	return tree
}

func resetService(t *testing.T, tree string, rounds roundReadDeleter) *RefreshService {
	t.Helper()

	agents := &recordingRebaseAgents{agent: agentstore.Agent{
		ID: "a1", Name: "auth", WorkflowID: "w1", Cwd: tree, State: agentstore.StateIdle,
	}}

	return NewRefreshService(NewRefreshServiceParams{
		Agents:    agents,
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Rounds:    rounds,
	})
}

// TestResetKeepsWhatNothingCouldRecover covers the one thing a park cannot hold, since it is
// built with git add -A and so never sees an ignored file.
func TestResetKeepsWhatNothingCouldRecover(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	service := resetService(t, tree, nil)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)
	require.NotEmpty(t, result.ParkedRef)

	assert.Equal(t, "SECRET=hunter2\n", readTree(t, tree, "secrets.env"), "the ignored file is still there")
	assert.Equal(t, "untracked\n", readTree(t, tree, "scratch.txt"), "and so is the untracked one")

	inPark, err := git.Run(t.Context(), tree, "cat-file", "-e", result.ParkedRef+":secrets.env")
	require.Error(t, err, "the park cannot hold an ignored file, which is why it must not be removed")
	assert.Empty(t, inPark)
}

// TestResetDiscardsTheWorkAndParksIt covers the tracked half, where everything that goes has to
// be reachable from the park afterwards.
func TestResetDiscardsTheWorkAndParksIt(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	before, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)

	service := resetService(t, tree, nil)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)
	require.Equal(t, 1, result.Commits, "the fixture did not produce the commit this is about")
	require.True(t, result.TrackedChanges, "nor the uncommitted edit")

	assert.Equal(t, "base\n", readTree(t, tree, "tracked.txt"), "the uncommitted edit is gone")
	assert.NoFileExists(t, filepath.Join(tree, "attempt.txt"), "and so is the commit's file")

	parent, err := git.Run(t.Context(), tree, "rev-parse", result.ParkedRef+"^")
	require.NoError(t, err)
	assert.Equal(t, before, parent, "the discarded commit is reachable from the park")

	kept, err := git.Run(t.Context(), tree, "show", result.ParkedRef+":tracked.txt")
	require.NoError(t, err)
	assert.Equal(t, "edited and not committed", kept, "and so is the uncommitted edit")
}

// TestResetDoesNotRunWhenTheParkFails pins the ordering the whole safety argument rests on.
func TestResetDoesNotRunWhenTheParkFails(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	before, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)

	// A ref already standing where the park wants to go, with a directory in the way of the
	// name, is a park that cannot be written.
	gitRun(t, tree, "update-ref", "refs/weave/prerefresh/a1", before)

	service := resetService(t, tree, nil)

	_, err = service.Reset(t.Context(), "a1")
	require.ErrorIs(t, err, ErrResetPark)

	after, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, before, after, "nothing was reset")
	assert.Equal(t, "edited and not committed\n", readTree(t, tree, "tracked.txt"), "and nothing was discarded")
}

// TestResetTearsDownTheReviewHistory covers the rounds going, since they survive a reset on
// their own and would describe work that no longer exists.
func TestResetTearsDownTheReviewHistory(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	snapshot, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)
	gitRun(t, tree, "update-ref", roundRef("a1", snapshot), snapshot)

	rounds := &fakeRounds{rounds: []reviewstore.Round{{AgentID: "a1", SnapshotSHA: snapshot}}}
	service := resetService(t, tree, rounds)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)

	assert.Equal(t, 1, result.Rounds)
	assert.True(t, rounds.deleted, "the rows went with the refs")

	_, err = git.Run(t.Context(), tree, "rev-parse", "--verify", roundRef("a1", snapshot))
	require.Error(t, err, "the snapshot ref was released rather than left pinning an abandoned worktree")
}

func readTree(t *testing.T, dir, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)

	return string(body)
}

type fakeRounds struct {
	rounds  []reviewstore.Round
	deleted bool
}

func (f *fakeRounds) ListRounds(context.Context, string) ([]reviewstore.Round, error) {
	return f.rounds, nil
}

func (f *fakeRounds) DeleteRounds(context.Context, string) error {
	f.deleted = true

	return nil
}

// TestResetIsReachableOnADirtyTree covers the case a reset exists for, which refusing the plan
// on a dirty tree would put out of reach.
func TestResetIsReachableOnADirtyTree(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	service := resetService(t, tree, nil)

	plan, err := service.PlanRebase(t.Context(), "a1")
	require.NoError(t, err, "the plan is readable even though a replay could not run")
	assert.True(t, plan.TrackedChanges, "and says so, for the confirmation to name")

	_, err = service.Rebase(t.Context(), "a1")
	require.ErrorIs(t, err, ErrRebaseDirty, "the replay still refuses")

	_, err = service.Reset(t.Context(), "a1")
	require.NoError(t, err, "and the discard does not")
}

// TestResetRefusesWhatItCannotPark covers a path the index is holding back, whose content a
// park cannot read and so cannot give back.
func TestResetRefusesWhatItCannotPark(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	gitRun(t, tree, "update-index", "--assume-unchanged", "tracked.txt")

	service := resetService(t, tree, nil)

	_, err := service.Reset(t.Context(), "a1")
	require.ErrorIs(t, err, ErrRebaseHiddenPaths)
}
