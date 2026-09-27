package worktrees

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"komrad/internal/git"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInspectReadsABranchAgainstItsWorkflowsBase covers the workflow's base branch reaching
// git, and everything git answered being carried back: the standing, the changes, the
// commits, and the branch being kept since nothing else reaches its tip.
func TestInspectReadsABranchAgainstItsWorkflowsBase(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	develop := fixture.stores.SeedWorkflow(t, workflowstore.Config{Name: "develop", Worktree: workflowstore.WorktreeConfig{
		BaseBranch:  "develop",
		DiffExclude: []string{"proto/gen"},
	}})
	fixture.own(t, dir, agentstore.Agent{Name: "first", WorkflowID: develop.ID})
	fixture.git.State = git.TreeState{Branch: filepath.Base(dir), InProgress: true}
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	write(t, dir, "big/one", strings.Repeat("x", 20000))
	fixture.git.Changes = git.Changes{Modified: []string{"own.go"}, Untracked: []string{"scratch.txt"}}
	fixture.git.Ahead, fixture.git.Behind = 2, 12
	fixture.git.Prediction = git.Prediction{Landed: 1, Replay: 1}
	fixture.git.DiffStat = git.DiffStat{Added: 214, Removed: 87}
	fixture.git.Remote = git.RemoteStanding{Pushed: true, Unpushed: 1}
	fixture.git.Commits = []git.Commit{{
		SHA:         "abc",
		Subject:     "own work",
		CommittedAt: at,
	}}

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.Equal(t, "develop", fixture.git.Resolved)
	assert.Equal(t, 0, fixture.git.Fetches)
	assert.Equal(t, dir, inspection.Worktree.Dir)
	assert.Equal(t, "origin/develop", inspection.BaseRef)
	assert.Equal(t, 2, inspection.Ahead)
	assert.Equal(t, 12, inspection.Behind)
	assert.Equal(t, &git.DiffStat{Added: 214, Removed: 87}, inspection.DiffStat)
	assert.Equal(t, []string{"proto/gen"}, fixture.git.Excluded)
	assert.Equal(t, git.RemoteStanding{Pushed: true, Unpushed: 1}, inspection.Remote)
	assert.Equal(t, 1, inspection.Landed)
	assert.True(t, inspection.SequenceInProgress)
	assert.True(t, inspection.KeepsBranch)
	assert.Equal(t, fixture.git.Changes, inspection.Changes)
	assert.Equal(t, fixture.git.Commits, inspection.Commits)
	assert.Equal(t, Entry{
		Path:  "big",
		Bytes: usage(t, filepath.Join(dir, "big/one")),
		Dir:   true,
	}, inspection.Entries[0])
	assert.Equal(t, sumEntryBytes(inspection.Entries), inspection.Bytes)
}

// TestInspectAsksWhetherTheBaseHoldsWhatOriginHas covers a branch behind its copy on origin
// being asked about against the base, and a branch level with it not being asked.
func TestInspectAsksWhetherTheBaseHoldsWhatOriginHas(t *testing.T) {
	tests := []struct {
		name       string
		remote     git.RemoteStanding
		wantAsked  string
		wantLanded bool
	}{
		{
			name: "nothing unpulled is not asked about",
			remote: git.RemoteStanding{
				Pushed:   true,
				Unpushed: 2,
			},
		},
		{
			name: "unpulled commits the base holds have landed",
			remote: git.RemoteStanding{
				Pushed:   true,
				Unpushed: 4,
				Unpulled: 4,
			},
			wantAsked:  "origin/main",
			wantLanded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newFixture(t)
			dir := fixture.cut(t)
			fixture.own(t, dir, agentstore.Agent{Name: "squashed"})
			fixture.git.Remote = tt.remote
			fixture.git.RemoteLanded = true

			inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
			require.NoError(t, err)

			assert.Equal(t, tt.wantAsked, fixture.git.HoldAsked)
			assert.Equal(t, tt.wantLanded, inspection.RemoteLanded)
		})
	}
}

// TestInspectReportsABaseTheCloneDoesNotHold covers a clone without the workflow's base
// answering with why rather than with a standing against something else.
func TestInspectReportsABaseTheCloneDoesNotHold(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	fixture.own(t, dir, agentstore.Agent{Name: "stale"})
	fixture.git.Unresolved = true
	fixture.git.Commits = []git.Commit{{SHA: "never"}}
	fixture.git.DiffStat = git.DiffStat{Added: 1}
	fixture.git.Remote = git.RemoteStanding{Pushed: true, Unpulled: 3}

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	require.ErrorIs(t, inspection.BaseErr, git.ErrBaseMissing)
	assert.Empty(t, inspection.BaseRef)
	assert.Empty(t, inspection.Commits)
	assert.Nil(t, inspection.DiffStat)
	assert.Equal(t, git.RemoteStanding{Pushed: true, Unpulled: 3}, inspection.Remote)
	assert.False(t, inspection.KeepsBranch)
}

// TestInspectLeavesADetachedHeadWithNoRemote covers a checkout with no branch to push.
func TestInspectLeavesADetachedHeadWithNoRemote(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	fixture.own(t, dir, agentstore.Agent{Name: "detached"})
	write(t, filepath.Join(fixture.repo, ".git", "worktrees", filepath.Base(dir)), "HEAD", "0123456789abcdef0123456789abcdef01234567\n")
	fixture.git.Remote = git.RemoteStanding{Pushed: true, Unpushed: 2}

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.Empty(t, inspection.Worktree.Branch)
	assert.Zero(t, inspection.Remote)
}

// TestInspectReportsADirectoryWithNoRowAsHavingNoBase covers a directory whose agent row is
// gone, which names no workflow and so no base.
func TestInspectReportsADirectoryWithNoRowAsHavingNoBase(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	require.ErrorIs(t, inspection.BaseErr, git.ErrBaseMissing)
	assert.Empty(t, inspection.BaseRef)
}

// TestInspectFetchesWhenAsked covers the fetch flag going to git's fetch rather than its
// resolve, with the workflow's base named to it.
func TestInspectFetchesWhenAsked(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	fixture.own(t, dir, agentstore.Agent{Name: "fetched"})

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir, Fetch: true})
	require.NoError(t, err)

	assert.Equal(t, 0, fixture.git.Resolves)
	assert.Equal(t, 1, fixture.git.Fetches)
	assert.Equal(t, "main", fixture.git.Fetched)
	assert.Equal(t, "origin/main", inspection.BaseRef)
}

// TestInspectReadsALandedBranchAsLeavingNothingBehind covers a branch some other ref reaches
// not being kept.
func TestInspectReadsALandedBranchAsLeavingNothingBehind(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	fixture.own(t, dir, agentstore.Agent{Name: "landed"})
	fixture.git.Held = true

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.False(t, inspection.KeepsBranch)
}

// TestInspectKeepsAnAdoptedBranch covers the adopted flag winning over reachability.
func TestInspectKeepsAnAdoptedBranch(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	fixture.own(t, dir, agentstore.Agent{Name: "adopted", AdoptedBranch: "agent/" + filepath.Base(dir)})
	fixture.git.Held = true

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.True(t, inspection.KeepsBranch)
}

// TestInspectReadsAnUnrelatedBaseAsNothingLanded covers git's refusal to relate the branch
// and the base being read as no landed commits rather than as a failure.
func TestInspectReadsAnUnrelatedBaseAsNothingLanded(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	fixture.own(t, dir, agentstore.Agent{Name: "unrelated"})
	fixture.git.PredictErr = git.ErrBaseUnrelated
	fixture.git.DiffErr = git.ErrBaseUnrelated

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.Equal(t, 0, inspection.Landed)
	assert.Nil(t, inspection.DiffStat, "an unrelated base measures no lines")
}

// TestInspectReadsOnlyTheEntriesOfAGoneRegistration covers a directory git cannot read
// answering with its entries and nothing from git.
func TestInspectReadsOnlyTheEntriesOfAGoneRegistration(t *testing.T) {
	fixture := newFixture(t)
	dir := filepath.Join(fixture.shared, "ffff")
	write(t, dir, ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/ffff\n")
	write(t, dir, "left", strings.Repeat("x", 20000))
	fixture.git.Commits = []git.Commit{{SHA: "never"}}

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.False(t, inspection.Worktree.Readable)
	assert.Empty(t, inspection.BaseRef)
	assert.Equal(t, Entry{Path: "left", Bytes: usage(t, filepath.Join(dir, "left"))}, inspection.Entries[0])
	assert.Empty(t, inspection.Commits)
}

// TestInspectRefusesADirectoryTheListingDoesNotHold covers a path outside the worktree
// directories being not found rather than read.
func TestInspectRefusesADirectoryTheListingDoesNotHold(t *testing.T) {
	fixture := newFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.shared, "gggg"), 0o755))

	_, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: fixture.repo})

	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestInspectRecordsTheSizeItWalked covers the walk's total being written to the size store
// under the time of the call and carried back on the worktree, over whatever was stored.
func TestInspectRecordsTheSizeItWalked(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	require.NoError(t, fixture.worktrees.Upsert(t.Context(), worktreestore.Worktree{Dir: dir}))
	require.NoError(t, fixture.worktrees.RecordSize(t.Context(), worktreestore.Size{
		Dir:        dir,
		MeasuredAt: fixture.now.Add(-time.Hour),
		Error:      "permission denied",
	}))

	inspection, err := fixture.newInspector().Inspect(t.Context(), InspectParams{Dir: dir})
	require.NoError(t, err)

	assert.Positive(t, inspection.Bytes)
	stored := fixture.readSize(t, dir)
	assert.Equal(t, inspection.Bytes, stored.Bytes)
	assert.Empty(t, stored.Error)
	assert.True(t, stored.MeasuredAt.Equal(fixture.now))
	require.NotNil(t, inspection.Worktree.Size)
	assert.Equal(t, inspection.Bytes, inspection.Worktree.Size.Bytes)
}

// sumEntryBytes adds up what the entries hold.
func sumEntryBytes(entries []Entry) int64 {
	var total int64
	for _, entry := range entries {
		total += entry.Bytes
	}

	return total
}
