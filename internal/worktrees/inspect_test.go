package worktrees

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/git"
	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInspectReadsABranchAgainstItsWorkflowsBase covers the workflow's base branch reaching
// git, and everything git answered being carried back: the standing, the changes, the
// commits, and the branch being kept since nothing else reaches its tip.
func TestInspectReadsABranchAgainstItsWorkflowsBase(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "aaaa")
	f.own("aaaa", "first", nil)
	f.flows.flows["flow"] = &workflowstore.Workflow{Worktree: workflowstore.WorktreeConfig{BaseBranch: "develop"}}
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	write(t, dir, "big/one", strings.Repeat("x", 300))
	f.git.changes = git.Changes{Modified: []string{"own.go"}, Untracked: []string{"scratch.txt"}}
	f.git.state = git.TreeState{Branch: "agent/aaaa", InProgress: true}
	f.git.ahead, f.git.behind = 2, 12
	f.git.prediction = git.Prediction{Landed: 1, Replay: 1}
	f.git.commits = []git.Commit{{SHA: "abc", Subject: "own work", CommittedAt: at}}

	inspection, err := f.inspector().Inspect(t.Context(), dir, false)
	require.NoError(t, err)

	assert.Equal(t, "develop", f.git.resolved)
	assert.Equal(t, 0, f.git.fetches)
	assert.Equal(t, dir, inspection.Worktree.Dir)
	assert.Equal(t, "origin/develop", inspection.BaseRef)
	assert.Equal(t, 2, inspection.Ahead)
	assert.Equal(t, 12, inspection.Behind)
	assert.Equal(t, 1, inspection.Landed)
	assert.True(t, inspection.SequenceInProgress)
	assert.True(t, inspection.KeepsBranch)
	assert.Equal(t, f.git.changes, inspection.Changes)
	assert.Equal(t, f.git.commits, inspection.Commits)
	assert.Equal(t, Entry{Path: "big", Bytes: 300, Dir: true}, inspection.Entries[0])
	assert.Equal(t, held(inspection.Entries), inspection.Bytes)
}

// held is what the entries add up to.
func held(entries []Entry) int64 {
	var total int64
	for _, entry := range entries {
		total += entry.Bytes
	}

	return total
}

// TestInspectReadsNothingAgainstABaseGitCannotName covers a clone with no origin and no base
// answering with no base and no standing rather than a standing against HEAD.
func TestInspectReadsNothingAgainstABaseGitCannotName(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "hhhh")
	f.git.unresolved = true
	f.git.commits = []git.Commit{{SHA: "never"}}

	inspection, err := f.inspector().Inspect(t.Context(), dir, false)
	require.NoError(t, err)

	assert.Empty(t, inspection.BaseRef)
	assert.Empty(t, inspection.Commits)
	assert.False(t, inspection.KeepsBranch)
}

// TestInspectFetchesWhenAsked covers the fetch flag going to git's fetch rather than its
// resolve, and a directory with no row measuring against origin's own head.
func TestInspectFetchesWhenAsked(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "bbbb")

	inspection, err := f.inspector().Inspect(t.Context(), dir, true)
	require.NoError(t, err)

	assert.Equal(t, 0, f.git.resolves)
	assert.Equal(t, 1, f.git.fetches)
	assert.Empty(t, f.git.fetched)
	assert.Equal(t, "origin/", inspection.BaseRef)
}

// TestInspectReadsALandedBranchAsLeavingNothingBehind covers a branch some other ref reaches
// not being kept.
func TestInspectReadsALandedBranchAsLeavingNothingBehind(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "cccc")
	f.git.held = true

	inspection, err := f.inspector().Inspect(t.Context(), dir, false)
	require.NoError(t, err)

	assert.False(t, inspection.KeepsBranch)
}

// TestInspectKeepsAnAdoptedBranch covers the adopted flag winning over reachability.
func TestInspectKeepsAnAdoptedBranch(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "dddd")
	f.own("dddd", "adopted", nil)
	f.owners.rows[0].AdoptedBranch = "agent/dddd"
	f.git.held = true

	inspection, err := f.inspector().Inspect(t.Context(), dir, false)
	require.NoError(t, err)

	assert.True(t, inspection.KeepsBranch)
}

// TestInspectReadsAnUnrelatedBaseAsNothingLanded covers git's refusal to relate the branch
// and the base being read as no landed commits rather than as a failure.
func TestInspectReadsAnUnrelatedBaseAsNothingLanded(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t, "eeee")
	f.git.predictErr = git.ErrBaseUnrelated

	inspection, err := f.inspector().Inspect(t.Context(), dir, false)
	require.NoError(t, err)

	assert.Equal(t, 0, inspection.Landed)
}

// TestInspectReadsOnlyTheEntriesOfAGoneRegistration covers a directory git cannot read
// answering with its entries and nothing from git.
func TestInspectReadsOnlyTheEntriesOfAGoneRegistration(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.shared, "ffff")
	write(t, dir, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/ffff\n")
	write(t, dir, "left", strings.Repeat("x", 100))
	f.git.commits = []git.Commit{{SHA: "never"}}

	inspection, err := f.inspector().Inspect(t.Context(), dir, false)
	require.NoError(t, err)

	assert.False(t, inspection.Worktree.Readable)
	assert.Empty(t, inspection.BaseRef)
	assert.Equal(t, Entry{Path: "left", Bytes: 100}, inspection.Entries[0])
	assert.Empty(t, inspection.Commits)
}

// TestInspectRefusesADirectoryTheListingDoesNotHold covers a path outside the worktree
// directories being not found rather than read.
func TestInspectRefusesADirectoryTheListingDoesNotHold(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.shared, "gggg"), 0o755))

	_, err := f.inspector().Inspect(t.Context(), f.repo, false)

	require.ErrorIs(t, err, store.ErrNotFound)
}
