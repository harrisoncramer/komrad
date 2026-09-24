package worktrees

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/git"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	worktreestore "weave/internal/store/worktree"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInspectReadsABranchAgainstItsWorkflowsBase covers the workflow's base branch reaching
// git, and everything git answered being carried back: the standing, the changes, the
// commits, and the branch being kept since nothing else reaches its tip.
func TestInspectReadsABranchAgainstItsWorkflowsBase(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t)
	develop := f.stores.SeedWorkflow(t, workflowstore.Config{Name: "develop", Worktree: workflowstore.WorktreeConfig{BaseBranch: "develop"}})
	f.own(t, dir, agentstore.Agent{Name: "first", WorkflowID: develop.ID})
	f.git.State = git.TreeState{Branch: filepath.Base(dir), InProgress: true}
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	write(t, dir, "big/one", strings.Repeat("x", 20000))
	f.git.Changes = git.Changes{Modified: []string{"own.go"}, Untracked: []string{"scratch.txt"}}
	f.git.Ahead, f.git.Behind = 2, 12
	f.git.Prediction = git.Prediction{Landed: 1, Replay: 1}
	f.git.Commits = []git.Commit{{
		SHA:         "abc",
		Subject:     "own work",
		CommittedAt: at,
	}}

	inspection, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.Equal(t, "develop", f.git.Resolved)
	assert.Equal(t, 0, f.git.Fetches)
	assert.Equal(t, dir, inspection.Worktree.Dir)
	assert.Equal(t, "origin/develop", inspection.BaseRef)
	assert.Equal(t, 2, inspection.Ahead)
	assert.Equal(t, 12, inspection.Behind)
	assert.Equal(t, 1, inspection.Landed)
	assert.True(t, inspection.SequenceInProgress)
	assert.True(t, inspection.KeepsBranch)
	assert.Equal(t, f.git.Changes, inspection.Changes)
	assert.Equal(t, f.git.Commits, inspection.Commits)
	assert.Equal(t, Entry{
		Path:  "big",
		Bytes: usage(t, filepath.Join(dir, "big/one")),
		Dir:   true,
	}, inspection.Entries[0])
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

// TestInspectReportsABaseTheCloneDoesNotHold covers a clone without the workflow's base
// answering with why rather than with a standing against something else.
func TestInspectReportsABaseTheCloneDoesNotHold(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t)
	f.own(t, dir, agentstore.Agent{Name: "stale"})
	f.git.Unresolved = true
	f.git.Commits = []git.Commit{{SHA: "never"}}

	inspection, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	require.ErrorIs(t, inspection.BaseErr, git.ErrBaseMissing)
	assert.Empty(t, inspection.BaseRef)
	assert.Empty(t, inspection.Commits)
	assert.False(t, inspection.KeepsBranch)
}

// TestInspectReportsADirectoryWithNoRowAsHavingNoBase covers a directory whose agent row is
// gone, which names no workflow and so no base.
func TestInspectReportsADirectoryWithNoRowAsHavingNoBase(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t)

	inspection, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	require.ErrorIs(t, inspection.BaseErr, git.ErrBaseMissing)
	assert.Empty(t, inspection.BaseRef)
}

// TestInspectFetchesWhenAsked covers the fetch flag going to git's fetch rather than its
// resolve, with the workflow's base named to it.
func TestInspectFetchesWhenAsked(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t)
	f.own(t, dir, agentstore.Agent{Name: "fetched"})

	inspection, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: dir, Fetch: true})
	require.NoError(t, err)

	assert.Equal(t, 0, f.git.Resolves)
	assert.Equal(t, 1, f.git.Fetches)
	assert.Equal(t, "main", f.git.Fetched)
	assert.Equal(t, "origin/main", inspection.BaseRef)
}

// TestInspectReadsALandedBranchAsLeavingNothingBehind covers a branch some other ref reaches
// not being kept.
func TestInspectReadsALandedBranchAsLeavingNothingBehind(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t)
	f.own(t, dir, agentstore.Agent{Name: "landed"})
	f.git.Held = true

	inspection, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.False(t, inspection.KeepsBranch)
}

// TestInspectKeepsAnAdoptedBranch covers the adopted flag winning over reachability.
func TestInspectKeepsAnAdoptedBranch(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t)
	f.own(t, dir, agentstore.Agent{Name: "adopted", AdoptedBranch: "agent/" + filepath.Base(dir)})
	f.git.Held = true

	inspection, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.True(t, inspection.KeepsBranch)
}

// TestInspectReadsAnUnrelatedBaseAsNothingLanded covers git's refusal to relate the branch
// and the base being read as no landed commits rather than as a failure.
func TestInspectReadsAnUnrelatedBaseAsNothingLanded(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t)
	f.own(t, dir, agentstore.Agent{Name: "unrelated"})
	f.git.PredictErr = git.ErrBaseUnrelated

	inspection, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.Equal(t, 0, inspection.Landed)
}

// TestInspectReadsOnlyTheEntriesOfAGoneRegistration covers a directory git cannot read
// answering with its entries and nothing from git.
func TestInspectReadsOnlyTheEntriesOfAGoneRegistration(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.shared, "ffff")
	write(t, dir, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/ffff\n")
	write(t, dir, "left", strings.Repeat("x", 20000))
	f.git.Commits = []git.Commit{{SHA: "never"}}

	inspection, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.False(t, inspection.Worktree.Readable)
	assert.Empty(t, inspection.BaseRef)
	assert.Equal(t, Entry{Path: "left", Bytes: usage(t, filepath.Join(dir, "left"))}, inspection.Entries[0])
	assert.Empty(t, inspection.Commits)
}

// TestInspectRefusesADirectoryTheListingDoesNotHold covers a path outside the worktree
// directories being not found rather than read.
func TestInspectRefusesADirectoryTheListingDoesNotHold(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(f.shared, "gggg"), 0o755))

	_, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: f.repo})

	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestInspectRecordsTheSizeItWalked covers the walk's total being written to the size store
// under the time of the call and carried back on the worktree, over whatever was stored.
func TestInspectRecordsTheSizeItWalked(t *testing.T) {
	f := newFixture(t)
	dir := f.cut(t)
	require.NoError(t, f.worktrees.Upsert(t.Context(), worktreestore.Worktree{Dir: dir}))
	require.NoError(t, f.worktrees.RecordSize(t.Context(), worktreestore.Size{
		Dir:        dir,
		MeasuredAt: f.now.Add(-time.Hour),
		Error:      "permission denied",
	}))

	inspection, err := f.inspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.Positive(t, inspection.Bytes)
	stored := f.stored(t)[dir].Size
	require.NotNil(t, stored)
	assert.Equal(t, inspection.Bytes, stored.Bytes)
	assert.Empty(t, stored.Error)
	assert.True(t, stored.MeasuredAt.Equal(f.now))
	require.NotNil(t, inspection.Worktree.Size)
	assert.Equal(t, inspection.Bytes, inspection.Worktree.Size.Bytes)
}
