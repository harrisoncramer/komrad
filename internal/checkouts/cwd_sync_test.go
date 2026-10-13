package checkouts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"komrad/internal/git"
	"komrad/internal/gittest"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
	"komrad/internal/storetest"
	"komrad/internal/worktrees"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncFixture is a real store with a workflow and the review store.
type syncFixture struct {
	stores     *storetest.Stores
	reviews    *reviewstore.ReviewStore
	worktrees  *worktreestore.WorktreeStore
	workflowID string
}

// countingPullRequestObserver counts the observation passes it is asked for.
type countingPullRequestObserver struct {
	passes int
}

func (c *countingPullRequestObserver) Observe(context.Context) (int64, error) {
	c.passes++

	return 0, nil
}

// TestCwdSyncerSyncRecordsTheCheckout covers what a pass writes of a live agent's checkout.
func TestCwdSyncerSyncRecordsTheCheckout(t *testing.T) {
	const komrad = "https://github.com/harrisoncramer/komrad"
	const other = "https://gitlab.com/group/subgroup/repo"

	tests := []struct {
		name        string
		before      taskstore.SetHeadParams
		cwd         func(t *testing.T) string
		head        git.HeadState
		headErr     error
		wantBranch  *string
		wantRepo    *string
		wantSHA     *string
		wantRemoved bool
	}{
		{
			name: "a blank cwd carries nothing and is not reported removed",
			cwd: func(*testing.T) string {
				return ""
			},
		},
		{
			name: "a directory that is not a checkout carries nothing",
			cwd: func(t *testing.T) string {
				return t.TempDir()
			},
		},
		{
			name: "a cwd that no longer exists is reported removed and cleared",
			before: taskstore.SetHeadParams{
				Branch:        xptr.Ptr("main"),
				RepositoryURL: xptr.Ptr(komrad),
				HeadSHA:       xptr.Ptr("aaa"),
			},
			cwd: makeRemovedDir,
			head: git.HeadState{
				Branch:        "main",
				RepositoryURL: komrad,
				SHA:           "aaa",
			},
			wantRemoved: true,
		},
		{
			name: "a checkout git cannot read in time keeps what an earlier pass wrote",
			before: taskstore.SetHeadParams{
				Branch:        xptr.Ptr("main"),
				RepositoryURL: xptr.Ptr(komrad),
				HeadSHA:       xptr.Ptr("aaa"),
			},
			cwd:        makeCheckout,
			headErr:    context.DeadlineExceeded,
			wantBranch: xptr.Ptr("main"),
			wantRepo:   xptr.Ptr(komrad),
			wantSHA:    xptr.Ptr("aaa"),
		},
		{
			name:       "a checkout whose origin is not a forge clears what an earlier pass wrote",
			before:     taskstore.SetHeadParams{RepositoryURL: xptr.Ptr(komrad)},
			cwd:        makeCheckout,
			head:       git.HeadState{Branch: "main"},
			wantBranch: xptr.Ptr("main"),
		},
		{
			name: "a checkout records its branch, canonical address and commit over what an earlier pass wrote",
			before: taskstore.SetHeadParams{
				Branch:        xptr.Ptr("main"),
				RepositoryURL: xptr.Ptr(komrad),
				HeadSHA:       xptr.Ptr("aaa"),
			},
			cwd: makeCheckout,
			head: git.HeadState{
				Branch:        "main",
				RepositoryURL: other,
				SHA:           "bbb",
			},
			wantBranch: xptr.Ptr("main"),
			wantRepo:   xptr.Ptr(other),
			wantSHA:    xptr.Ptr("bbb"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSyncFixture(t)
			cwd := tt.cwd(t)
			agent := fixture.seed(t, "a", cwd)
			fixture.stores.SeedTaskHead(t, agent.ID, tt.before)

			heads := buildHeads(cwd, tt.head)
			heads.Err = tt.headErr

			err := fixture.newSyncer(newSyncerParams{Heads: heads}).Sync(t.Context())
			if tt.headErr != nil {
				require.ErrorIs(t, err, tt.headErr)
			} else {
				require.NoError(t, err)
			}

			got := fixture.read(t, agent.ID)
			assert.Equal(t, tt.wantRemoved, got.WorktreeRemoved)
			assert.Equal(t, tt.wantBranch, got.Branch)
			assert.Equal(t, tt.wantRepo, got.RepositoryURL)
			assert.Equal(t, tt.wantSHA, got.HeadSHA)
		})
	}
}

// TestCwdSyncerSyncObservesANewBranch covers a pass observing pull requests only when a branch moved.
func TestCwdSyncerSyncObservesANewBranch(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	fixture.seed(t, "a", cwd)

	heads := buildHeads(cwd, git.HeadState{Branch: "agent/x"})
	observations := &countingPullRequestObserver{}
	syncer := fixture.newSyncer(newSyncerParams{Heads: heads, PullRequests: observations})

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, observations.passes, "recording a new branch observes")

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, observations.passes, "a branch already on the row observes nothing")

	heads.States[cwd] = git.HeadState{Branch: "agent/y"}
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 2, observations.passes, "moving branch observes again")
}

// TestCwdSyncerSyncLooksAtEveryAgent covers the fan-out: a pass over more agents than it has
// workers still reads every directory and writes every row.
func TestCwdSyncerSyncLooksAtEveryAgent(t *testing.T) {
	fixture := newSyncFixture(t)
	heads := &gittest.Heads{States: map[string]git.HeadState{}}
	count := cwdLookWorkers*2 + 1
	for i := range count {
		cwd := makeCheckout(t)
		heads.States[cwd] = git.HeadState{Branch: fmt.Sprintf("b%d", i)}
		fixture.seed(t, fmt.Sprintf("a%d", i), cwd)
	}

	require.NoError(t, fixture.newSyncer(newSyncerParams{Heads: heads}).Sync(t.Context()))

	agents, err := fixture.stores.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, count)
	for _, agent := range agents {
		got := fixture.read(t, agent.ID)
		require.NotNil(t, got.Branch, agent.Name)
		assert.Equal(t, heads.States[agent.Cwd].Branch, *got.Branch)
	}
}

// TestCwdSyncerSyncRecordsAStoppedSequence covers a rebase part-way through a checkout reaching the row.
func TestCwdSyncerSyncRecordsAStoppedSequence(t *testing.T) {
	fixture := newSyncFixture(t)
	repo := makeCheckout(t)
	agent := fixture.seed(t, "a", repo)

	require.NoError(t, fixture.newSyncer(newSyncerParams{Heads: buildHeads(repo, git.HeadState{Branch: "topic", Sequence: git.SequenceMerge})}).Sync(t.Context()))

	got := fixture.read(t, agent.ID)
	assert.Equal(t, taskstore.SequenceMerge, got.Sequence)
	assert.Equal(t, xptr.Ptr("topic"), got.Branch)

	require.NoError(t, fixture.newSyncer(newSyncerParams{Heads: buildHeads(repo, git.HeadState{Branch: "topic"})}).Sync(t.Context()))

	assert.Equal(t, taskstore.SequenceNone, fixture.read(t, agent.ID).Sequence)
}

// TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd covers komrad never asking a gone cwd for its branch.
func TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd(t *testing.T) {
	fixture := newSyncFixture(t)
	fixture.seed(t, "a", makeRemovedDir(t))
	looked := &gittest.Heads{}

	require.NoError(t, fixture.newSyncer(newSyncerParams{Heads: looked}).Sync(t.Context()))

	assert.Empty(t, looked.Dirs)
}

// TestCwdSyncerSyncClearsWhatAnEarlierPassWrote covers komrad clearing a branch and a removal once the cwd moves.
func TestCwdSyncerSyncClearsWhatAnEarlierPassWrote(t *testing.T) {
	fixture := newSyncFixture(t)
	repo := makeCheckout(t)
	onBranch := fixture.seed(t, "on-branch", repo)
	removed := fixture.seed(t, "removed", makeRemovedDir(t))
	syncer := fixture.newSyncer(newSyncerParams{Heads: buildHeads(repo, git.HeadState{Branch: "main"})})

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, xptr.Ptr("main"), fixture.read(t, onBranch.ID).Branch)
	require.True(t, fixture.read(t, removed.ID).WorktreeRemoved)

	for _, agent := range []*agentstore.Agent{onBranch, removed} {
		require.NoError(t, fixture.stores.Tasks.SetCheckoutDir(t.Context(), *agent.TaskID, t.TempDir()))
	}

	require.NoError(t, syncer.Sync(t.Context()))

	assert.Nil(t, fixture.read(t, onBranch.ID).Branch)
	assert.False(t, fixture.read(t, removed.ID).WorktreeRemoved)
}

// TestCwdSyncerSyncSkipsArchivedAgents covers that the pass walks live agents only.
func TestCwdSyncerSyncSkipsArchivedAgents(t *testing.T) {
	fixture := newSyncFixture(t)
	checkout := makeCheckout(t)
	gone := makeRemovedDir(t)
	ids := []string{fixture.seed(t, "checkout", checkout).ID, fixture.seed(t, "gone", gone).ID}
	_, err := fixture.stores.Agents.Archive(t.Context(), ids)
	require.NoError(t, err)

	heads := &gittest.Heads{States: map[string]git.HeadState{
		checkout: {Branch: "main"},
		gone:     {Branch: "main"},
	}}
	require.NoError(t, fixture.newSyncer(newSyncerParams{Heads: heads}).Sync(t.Context()))

	for _, id := range ids {
		got := fixture.read(t, id)
		assert.Nil(t, got.Branch)
		assert.False(t, got.WorktreeRemoved)
	}
}

// TestCwdSyncerSyncRecordsAMoveSinceTheNewestRound covers komrad writing the newest round once the tree no longer hashes to it.
func TestCwdSyncerSyncRecordsAMoveSinceTheNewestRound(t *testing.T) {
	tests := []struct {
		name         string
		newest       string
		snapshotTree string
		probe        string
		want         string
	}{
		{
			name:   "a round git cannot read records no move",
			newest: "snapshot-one",
			probe:  "tree-moved",
		},
		{
			name:  "an agent with no round records no move",
			probe: "tree-moved",
		},
		{
			name:         "a worktree that still hashes to its newest round records no move",
			newest:       "snapshot-one",
			snapshotTree: "tree-one",
			probe:        "tree-one",
		},
		{
			name:         "a worktree that moved records its newest round",
			newest:       "snapshot-one",
			snapshotTree: "tree-one",
			probe:        "tree-moved",
			want:         "snapshot-one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSyncFixture(t)
			cwd := makeCheckout(t)
			agent := fixture.seed(t, "a", cwd)
			if tt.newest != "" {
				fixture.openRound(t, agent.ID, tt.newest)
			}

			heads := buildHeads(cwd, git.HeadState{Root: cwd, SnapshotTree: tt.snapshotTree})
			trees := &gittest.Trees{Probes: map[string]string{cwd: tt.probe}}
			require.NoError(t, fixture.newSyncer(newSyncerParams{Heads: heads, Trees: trees}).Sync(t.Context()))

			assert.Equal(t, tt.want, fixture.read(t, agent.ID).MovedSinceRound)
		})
	}
}

// TestCwdSyncerSyncKeepsAMoveItCouldNotProbe covers a failed probe leaving the recorded move alone.
func TestCwdSyncerSyncKeepsAMoveItCouldNotProbe(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	agent := fixture.seed(t, "a", cwd)
	fixture.openRound(t, agent.ID, "snapshot-one")
	trees := &gittest.Trees{Probes: map[string]string{cwd: "tree-moved"}}
	syncer := fixture.newSyncer(newSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd, SnapshotTree: "tree-one"}), Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, "snapshot-one", fixture.read(t, agent.ID).MovedSinceRound)

	trees.ProbeErr = errors.New("the index could not be read")
	err := syncer.Sync(t.Context())

	require.ErrorContains(t, err, "failed to hash the worktree of task")
	assert.Equal(t, "snapshot-one", fixture.read(t, agent.ID).MovedSinceRound)
}

// TestCwdSyncerSyncRecordsTheDiffStat covers the lines a pass measures against the workflow's base.
func TestCwdSyncerSyncRecordsTheDiffStat(t *testing.T) {
	tests := []struct {
		name         string
		cwd          func(t *testing.T) string
		diffStats    func(cwd string) map[string]git.DiffStat
		probe        string
		headErr      error
		want         *taskstore.DiffStat
		wantMeasured bool
	}{
		{
			name: "a cwd that no longer exists clears the stat",
			cwd:  makeRemovedDir,
			diffStats: func(cwd string) map[string]git.DiffStat {
				return map[string]git.DiffStat{cwd: {Added: 4}}
			},
		},
		{
			name:    "a checkout git cannot read in time keeps the stat an earlier pass wrote",
			cwd:     makeCheckout,
			headErr: context.DeadlineExceeded,
			diffStats: func(cwd string) map[string]git.DiffStat {
				return map[string]git.DiffStat{cwd: {Added: 4}}
			},
			want: &taskstore.DiffStat{Added: 1, Removed: 1},
		},
		{
			name: "a branch that shares no history with the base clears the stat",
			cwd:  makeCheckout,
			diffStats: func(string) map[string]git.DiffStat {
				return map[string]git.DiffStat{}
			},
			wantMeasured: true,
		},
		{
			name: "a checkout records what it adds and removes over what an earlier pass wrote",
			cwd:  makeCheckout,
			diffStats: func(cwd string) map[string]git.DiffStat {
				return map[string]git.DiffStat{cwd: {Added: 12, Removed: 3}}
			},
			want:         &taskstore.DiffStat{Added: 12, Removed: 3},
			wantMeasured: true,
		},
		{
			name:  "a checkout with a round measures the tree its probe already hashed",
			cwd:   makeCheckout,
			probe: "probed-tree",
			diffStats: func(cwd string) map[string]git.DiffStat {
				return map[string]git.DiffStat{cwd: {Added: 12, Removed: 3}}
			},
			want:         &taskstore.DiffStat{Added: 12, Removed: 3},
			wantMeasured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSyncFixture(t)
			fixture.workflowID = fixture.stores.SeedWorkflow(t, workflowstore.Config{
				Name:     "measured",
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "trunk", DiffExclude: []string{"*.lock"}},
			}).ID
			cwd := tt.cwd(t)
			agent := fixture.seed(t, "a", cwd)
			require.NoError(t, fixture.stores.Tasks.SetDiffStat(t.Context(), *agent.TaskID, &taskstore.DiffStat{Added: 1, Removed: 1}))
			trees := &gittest.Trees{DiffStats: tt.diffStats(cwd)}
			head := git.HeadState{Root: cwd}
			if tt.probe != "" {
				trees.Probes = map[string]string{cwd: tt.probe}
				head.SnapshotTree = "round-tree"
			}

			heads := buildHeads(cwd, head)
			heads.Err = tt.headErr

			err := fixture.newSyncer(newSyncerParams{Heads: heads, Trees: trees}).Sync(t.Context())
			if tt.headErr != nil {
				require.ErrorIs(t, err, tt.headErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, fixture.read(t, agent.ID).DiffStat)
			var wantMeasured []git.ReadDiffStatParams
			if tt.wantMeasured {
				wantMeasured = []git.ReadDiffStatParams{{
					Dir:     cwd,
					Ref:     "origin/trunk",
					Tree:    tt.probe,
					Exclude: []string{"*.lock"},
				}}
			}
			assert.Equal(t, wantMeasured, trees.Measured)
		})
	}
}

// TestCwdSyncerSyncKeepsADiffStatItCouldNotMeasure covers a failed measure leaving the recorded stat alone.
func TestCwdSyncerSyncKeepsADiffStatItCouldNotMeasure(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	agent := fixture.seed(t, "a", cwd)
	trees := &gittest.Trees{DiffStats: map[string]git.DiffStat{cwd: {Added: 7, Removed: 2}}}
	syncer := fixture.newSyncer(newSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd}), Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, &taskstore.DiffStat{Added: 7, Removed: 2}, fixture.read(t, agent.ID).DiffStat)

	trees.DiffErr = errors.New("the index could not be read")
	err := syncer.Sync(t.Context())

	require.ErrorContains(t, err, "failed to measure the checkout of task")
	assert.Equal(t, &taskstore.DiffStat{Added: 7, Removed: 2}, fixture.read(t, agent.ID).DiffStat)
}

// TestCwdSyncerSyncRecordsWhereTheWorktreeStands covers a pass reading where the worktree of a task's checkout stands.
func TestCwdSyncerSyncRecordsWhereTheWorktreeStands(t *testing.T) {
	earlier := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	now := earlier.Add(time.Hour)

	tests := []struct {
		name   string
		stored *worktreestore.Standing
		want   *worktreestore.Standing
	}{
		{
			name: "a reading the row already holds writes nothing",
			stored: &worktreestore.Standing{
				ReadAt:   earlier,
				Dirty:    true,
				Unlanded: 3,
			},
			want: &worktreestore.Standing{
				ReadAt:   earlier,
				Dirty:    true,
				Unlanded: 3,
			},
		},
		{
			name: "the task's worktree records where it stands",
			stored: &worktreestore.Standing{
				ReadAt:   earlier,
				Unlanded: 1,
			},
			want: &worktreestore.Standing{
				ReadAt:   now,
				Dirty:    true,
				Unlanded: 3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSyncFixture(t)
			fixture.workflowID = fixture.stores.SeedWorkflow(t, workflowstore.Config{
				Name:     "cut",
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
			}).ID
			cwd := makeCheckout(t)
			fixture.seed(t, "a", cwd)
			require.NoError(t, fixture.worktrees.Upsert(t.Context(), worktreestore.Worktree{Dir: cwd, Readable: true}))
			if tt.stored != nil {
				stored := *tt.stored
				stored.Dir = cwd
				require.NoError(t, fixture.worktrees.RecordStanding(t.Context(), stored))
			}
			trees := &gittest.Trees{
				Changes:    git.Changes{Modified: []string{"a.go"}},
				Prediction: git.Prediction{Replay: 3},
			}
			syncer := fixture.newSyncer(newSyncerParams{
				Heads: buildHeads(cwd, git.HeadState{Root: cwd}),
				Trees: trees,
				Checkouts: worktrees.NewCheckoutReader(worktrees.NewCheckoutReaderParams{
					Git: trees,
					Now: func() time.Time {
						return now
					},
				}),
			})

			require.NoError(t, syncer.Sync(t.Context()))

			stored, err := fixture.worktrees.List(t.Context())
			require.NoError(t, err)
			require.Len(t, stored, 1)
			if tt.want == nil {
				assert.Nil(t, stored[0].Standing)

				return
			}
			want := *tt.want
			want.Dir = cwd
			got := *stored[0].Standing
			got.ReadAt = got.ReadAt.UTC()
			assert.Equal(t, want, got)
		})
	}
}

// TestCwdSyncerSyncReadsTheBaseOncePerPass covers the standing, the landing walk and the diff stat
// sharing one reading of the base, and no reading when none of them will run.
func TestCwdSyncerSyncReadsTheBaseOncePerPass(t *testing.T) {
	tests := []struct {
		name          string
		root          bool
		owned         bool
		wantRead      bool
		wantPredicted int
		wantWalked    []string
		wantMeasured  int
	}{
		{
			name:          "a checkout read for every use reads the base once",
			root:          true,
			owned:         true,
			wantRead:      true,
			wantPredicted: 1,
			wantWalked:    []string{"snapshot-one"},
			wantMeasured:  1,
		},
		{
			name:          "an owned worktree git found no root for still reads the base for its standing",
			owned:         true,
			wantRead:      true,
			wantPredicted: 1,
		},
		{
			name: "an unowned checkout git found no root for reads no base",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSyncFixture(t)
			fixture.workflowID = fixture.stores.SeedWorkflow(t, workflowstore.Config{
				Name:     "shared",
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
			}).ID
			cwd := makeCheckout(t)
			agent := fixture.seed(t, "a", cwd)
			fixture.release(t, agent.ID, "snapshot-one")
			if tt.owned {
				require.NoError(t, fixture.worktrees.Upsert(t.Context(), worktreestore.Worktree{Dir: cwd, Readable: true}))
			}
			head := git.HeadState{SHA: "head-one"}
			if tt.root {
				head.Root = cwd
			}
			trees := &gittest.Trees{
				Tips:      map[string]string{cwd: "tip-one"},
				DiffStats: map[string]git.DiffStat{cwd: {Added: 4, Removed: 1}},
			}
			syncer := fixture.newSyncer(newSyncerParams{
				Heads:     buildHeads(cwd, head),
				Trees:     trees,
				Checkouts: worktrees.NewCheckoutReader(worktrees.NewCheckoutReaderParams{Git: trees}),
			})

			require.NoError(t, syncer.Sync(t.Context()))

			var wantBasesRead []string
			if tt.wantRead {
				wantBasesRead = []string{cwd}
			}
			assert.Equal(t, wantBasesRead, trees.BasesRead)
			assert.Equal(t, tt.wantPredicted, trees.Predicted)
			assert.Equal(t, tt.wantWalked, trees.Walked)
			assert.Len(t, trees.Measured, tt.wantMeasured)
		})
	}
}

// reuseScene is the checkout and fakes a reuse case changes between its passes.
type reuseScene struct {
	fixture *syncFixture
	trees   *gittest.Trees
	heads   *gittest.Heads
	cwd     string
}

// TestCwdSyncerSyncReusesWhatAnUnchangedWorktreeRead covers a pass reusing the last probe and diff
// stat while the head, the fingerprint, the base tip and the exclusions hold, and never reusing a
// read that failed.
func TestCwdSyncerSyncReusesWhatAnUnchangedWorktreeRead(t *testing.T) {
	measured := &taskstore.DiffStat{Added: 4, Removed: 1}

	tests := []struct {
		name         string
		setup        func(t *testing.T, scene reuseScene)
		change       func(t *testing.T, scene reuseScene)
		wantFirstErr bool
		wantProbed   int
		wantMeasured int
		wantDiffStat *taskstore.DiffStat
	}{
		{
			name:         "an unchanged worktree is read once",
			wantProbed:   1,
			wantMeasured: 1,
			wantDiffStat: measured,
		},
		{
			name: "a changed fingerprint is read again",
			change: func(_ *testing.T, scene reuseScene) {
				scene.trees.Fingerprints[scene.cwd] = "fingerprint-two"
			},
			wantProbed:   2,
			wantMeasured: 2,
			wantDiffStat: measured,
		},
		{
			name: "a moved head is read again",
			change: func(_ *testing.T, scene reuseScene) {
				state := scene.heads.States[scene.cwd]
				state.SHA = "head-two"
				scene.heads.States[scene.cwd] = state
			},
			wantProbed:   2,
			wantMeasured: 2,
			wantDiffStat: measured,
		},
		{
			name: "a moved base tip measures again on the tree already probed",
			change: func(_ *testing.T, scene reuseScene) {
				scene.trees.Tips[scene.cwd] = "tip-two"
			},
			wantProbed:   1,
			wantMeasured: 2,
			wantDiffStat: measured,
		},
		{
			name: "changed exclusions measure again on the tree already probed",
			change: func(t *testing.T, scene reuseScene) {
				require.NoError(t, scene.fixture.stores.Workflows.Update(t.Context(), scene.fixture.workflowID, workflowstore.Config{
					Name:     "reused",
					Worktree: workflowstore.WorktreeConfig{BaseBranch: "main", DiffExclude: []string{"*.lock"}},
				}))
			},
			wantProbed:   1,
			wantMeasured: 2,
			wantDiffStat: measured,
		},
		{
			name: "a worktree git cannot fingerprint is read every pass",
			change: func(_ *testing.T, scene reuseScene) {
				delete(scene.trees.Fingerprints, scene.cwd)
			},
			wantProbed:   2,
			wantMeasured: 2,
			wantDiffStat: measured,
		},
		{
			name: "a failed probe is not reused",
			setup: func(_ *testing.T, scene reuseScene) {
				scene.trees.ProbeErr = errors.New("the index could not be read")
			},
			change: func(_ *testing.T, scene reuseScene) {
				scene.trees.ProbeErr = nil
			},
			wantFirstErr: true,
			wantProbed:   2,
			wantMeasured: 1,
			wantDiffStat: measured,
		},
		{
			name: "a failed measure is not reused",
			setup: func(_ *testing.T, scene reuseScene) {
				scene.trees.DiffErr = errors.New("the base could not be read")
			},
			change: func(_ *testing.T, scene reuseScene) {
				scene.trees.DiffErr = nil
			},
			wantFirstErr: true,
			wantProbed:   1,
			wantMeasured: 2,
			wantDiffStat: measured,
		},
		{
			name: "an unrelated base is measured once",
			setup: func(_ *testing.T, scene reuseScene) {
				delete(scene.trees.DiffStats, scene.cwd)
			},
			wantProbed:   1,
			wantMeasured: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSyncFixture(t)
			fixture.workflowID = fixture.stores.SeedWorkflow(t, workflowstore.Config{
				Name:     "reused",
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
			}).ID
			cwd := makeCheckout(t)
			agent := fixture.seed(t, "a", cwd)
			fixture.release(t, agent.ID, "snapshot-one")
			scene := reuseScene{
				fixture: fixture,
				trees: &gittest.Trees{
					Probes:       map[string]string{cwd: "probed-tree"},
					Tips:         map[string]string{cwd: "tip-one"},
					Fingerprints: map[string]string{cwd: "fingerprint-one"},
					DiffStats:    map[string]git.DiffStat{cwd: {Added: 4, Removed: 1}},
				},
				heads: buildHeads(cwd, git.HeadState{
					Root:         cwd,
					SHA:          "head-one",
					SnapshotTree: "round-tree",
				}),
				cwd: cwd,
			}
			if tt.setup != nil {
				tt.setup(t, scene)
			}
			syncer := fixture.newSyncer(newSyncerParams{Heads: scene.heads, Trees: scene.trees})

			err := syncer.Sync(t.Context())
			if tt.wantFirstErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.change != nil {
				tt.change(t, scene)
			}
			require.NoError(t, syncer.Sync(t.Context()))

			assert.Len(t, scene.trees.Probed, tt.wantProbed)
			assert.Len(t, scene.trees.Measured, tt.wantMeasured)
			assert.Equal(t, tt.wantDiffStat, fixture.read(t, agent.ID).DiffStat)
		})
	}
}

// TestCwdSyncerSyncLandsTheRoundsTheBaseHolds covers the walk from the newest round down to the
// first one the base holds whole.
func TestCwdSyncerSyncLandsTheRoundsTheBaseHolds(t *testing.T) {
	tests := []struct {
		name         string
		unresolved   bool
		landed       map[string]bool
		wantWalked   []string
		wantUnlanded []int32
	}{
		{
			name:         "a base the clone does not have walks nothing",
			unresolved:   true,
			landed:       map[string]bool{"snapshot-three": true},
			wantUnlanded: []int32{3, 2, 1},
		},
		{
			name:         "a base holding no round walks them all and lands none",
			landed:       map[string]bool{},
			wantWalked:   []string{"snapshot-three", "snapshot-two", "snapshot-one"},
			wantUnlanded: []int32{3, 2, 1},
		},
		{
			name:         "a base holding a middle round lands it and every earlier one",
			landed:       map[string]bool{"snapshot-two": true},
			wantWalked:   []string{"snapshot-three", "snapshot-two"},
			wantUnlanded: []int32{3},
		},
		{
			name:         "a base holding the newest round lands them all",
			landed:       map[string]bool{"snapshot-three": true},
			wantWalked:   []string{"snapshot-three"},
			wantUnlanded: []int32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSyncFixture(t)
			cwd := makeCheckout(t)
			agent := fixture.seed(t, "a", cwd)
			for _, snapshot := range []string{"snapshot-one", "snapshot-two", "snapshot-three"} {
				fixture.release(t, agent.ID, snapshot)
			}
			trees := buildEmptyTrees()
			trees.Landed = tt.landed
			trees.Unresolved = tt.unresolved
			trees.Tips = map[string]string{cwd: "tip-one"}

			require.NoError(t, fixture.newSyncer(newSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd}), Trees: trees}).Sync(t.Context()))

			assert.Equal(t, tt.wantWalked, trees.Walked)
			assert.Equal(t, tt.wantUnlanded, fixture.readUnlanded(t, *agent.TaskID))
		})
	}
}

// TestCwdSyncerSyncWalksTheLandingAgainOnlyWhenSomethingMoved covers a pass skipping the walk while
// neither the base tip nor the newest round has changed.
func TestCwdSyncerSyncWalksTheLandingAgainOnlyWhenSomethingMoved(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	agent := fixture.seed(t, "a", cwd)
	fixture.release(t, agent.ID, "snapshot-one")
	trees := buildEmptyTrees()
	trees.Tips = map[string]string{cwd: "tip-one"}
	syncer := fixture.newSyncer(newSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd}), Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, []string{"snapshot-one"}, trees.Walked, "an unmoved base is not walked twice")

	fixture.release(t, agent.ID, "snapshot-two")
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, []string{"snapshot-one", "snapshot-two", "snapshot-one"}, trees.Walked, "a new round is walked")

	trees.Tips[cwd] = "tip-two"
	trees.Landed = map[string]bool{"snapshot-two": true}
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, []string{"snapshot-one", "snapshot-two", "snapshot-one", "snapshot-two"}, trees.Walked, "a moved base is walked")
	assert.Empty(t, fixture.readUnlanded(t, *agent.TaskID))
}

// TestCwdSyncerSyncFetchesTheBaseOncePerMerge covers komrad fetching the base when the agent's pull
// request merges, and not again until another does or the fetch fails.
func TestCwdSyncerSyncFetchesTheBaseOncePerMerge(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	agent := fixture.seed(t, "a", cwd)
	fixture.release(t, agent.ID, "snapshot-one")
	trees := buildEmptyTrees()
	trees.Tips = map[string]string{cwd: "tip-one"}
	syncer := fixture.newSyncer(newSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd}), Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Empty(t, trees.Fetched, "an open pull request fetches nothing")

	fixture.merge(t, agent.ID, time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	trees.FetchErr = errors.New("network unreachable")
	require.Error(t, syncer.Sync(t.Context()))
	assert.Equal(t, []string{cwd}, trees.Fetched, "a merge fetches the base")

	trees.FetchErr = nil
	require.NoError(t, syncer.Sync(t.Context()))
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, []string{cwd, cwd}, trees.Fetched, "a failed fetch is tried again, and a fetched merge is not")
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()

	stores := storetest.Open(t)

	return &syncFixture{
		stores:     stores,
		reviews:    reviewstore.NewReviewStore(stores.Pool.GetDB()),
		worktrees:  worktreestore.NewWorktreeStore(stores.Pool.GetDB()),
		workflowID: stores.SeedWorkflow(t, workflowstore.Config{Name: "flow"}).ID,
	}
}

func (f *syncFixture) seed(t *testing.T, name, cwd string) *agentstore.Agent {
	t.Helper()

	agent := f.stores.SeedAgent(t, agentstore.Agent{
		Name:       name,
		WorkflowID: f.workflowID,
		Cwd:        cwd,
	})
	if cwd != "" {
		require.NoError(t, f.stores.Tasks.SetCheckoutDir(t.Context(), *agent.TaskID, cwd))
	}

	return agent
}

// read returns the checkout of the agent's task.
func (f *syncFixture) read(t *testing.T, agentID string) taskstore.Checkout {
	t.Helper()

	task, err := f.stores.Tasks.Get(t.Context(), f.taskOf(t, agentID))
	require.NoError(t, err)

	return task.Checkout
}

// taskOf is the task an agent works on.
func (f *syncFixture) taskOf(t *testing.T, agentID string) string {
	t.Helper()

	agent, err := f.stores.Agents.GetByID(t.Context(), agentID)
	require.NoError(t, err)
	require.NotNil(t, agent.TaskID)

	return *agent.TaskID
}

// release releases a review of a new round on the snapshot.
func (f *syncFixture) release(t *testing.T, agentID, snapshot string) {
	t.Helper()

	round := f.openRound(t, agentID, snapshot)
	review, err := f.reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		TaskID:     round.TaskID,
		RoundID:    round.ID,
		AuthorKind: reviewstore.AuthorHuman,
	})
	require.NoError(t, err)

	_, err = f.reviews.ReleaseReview(t.Context(), reviewstore.ReleaseReviewParams{
		ID:      review.ID,
		Verdict: reviewstore.VerdictCommented,
	})
	require.NoError(t, err)
}

// merge puts the agent on a branch whose pull request merged at the time given.
func (f *syncFixture) merge(t *testing.T, agentID string, mergedAt time.Time) {
	t.Helper()

	ctx := t.Context()
	repository, branch := "https://github.com/harrisoncramer/komrad", "agent/a"
	f.stores.SeedTaskHead(t, agentID, taskstore.SetHeadParams{
		RepositoryURL: &repository,
		Branch:        &branch,
	})
	require.NoError(t, f.stores.PullRequestSyncs.EnsureSync(ctx, repository))
	_, err := f.stores.PullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        "PR_1",
		URL:               repository + "/pull/1",
		RepositoryURL:     repository,
		HeadRepositoryURL: repository,
		Number:            1,
		HeadBranch:        branch,
		BaseBranch:        "main",
		State:             "merged",
		OpenedAt:          mergedAt.Add(-time.Hour),
		ChangedAt:         mergedAt,
		MergedAt:          &mergedAt,
	})
	require.NoError(t, err)
	_, err = f.stores.PullRequests.Observe(ctx)
	require.NoError(t, err)
}

func (f *syncFixture) openRound(t *testing.T, agentID, snapshot string) *reviewstore.Round {
	t.Helper()

	round, err := f.reviews.CreateRound(t.Context(), reviewstore.CreateRoundParams{
		TaskID:      f.taskOf(t, agentID),
		BaseSHA:     gittest.BaseSHA,
		SnapshotSHA: snapshot,
	})
	require.NoError(t, err)

	return round
}

type newSyncerParams struct {
	Heads        headReader
	Trees        *gittest.Trees
	Checkouts    *worktrees.CheckoutReader
	PullRequests pullRequestObserver
}

// newSyncer builds a syncer over the real stores, filling in whatever params leaves out.
func (f *syncFixture) newSyncer(params newSyncerParams) *CwdSyncer {
	if params.Heads == nil {
		params.Heads = &gittest.Heads{States: map[string]git.HeadState{}}
	}
	if params.Trees == nil {
		params.Trees = buildEmptyTrees()
	}
	if params.Checkouts == nil {
		params.Checkouts = worktrees.NewCheckoutReader(worktrees.NewCheckoutReaderParams{Git: params.Trees})
	}
	if params.PullRequests == nil {
		params.PullRequests = &countingPullRequestObserver{}
	}

	return NewCwdSyncer(NewCwdSyncerParams{
		Tasks:        f.stores.Tasks,
		Heads:        params.Heads,
		Trees:        params.Trees,
		Checkouts:    params.Checkouts,
		Rounds:       f.reviews,
		Workflows:    f.stores.Workflows,
		Worktrees:    f.worktrees,
		PullRequests: params.PullRequests,
	})
}

// readUnlanded is the ordinals of the task's rounds that have not landed, newest first.
func (f *syncFixture) readUnlanded(t *testing.T, taskID string) []int32 {
	t.Helper()

	unlanded, err := f.reviews.ListUnlandedRounds(t.Context())
	require.NoError(t, err)

	return xslice.Map(unlanded[taskID], func(round reviewstore.UnlandedRound) int32 {
		return round.Ordinal
	})
}

func buildHeads(cwd string, state git.HeadState) *gittest.Heads {
	return &gittest.Heads{States: map[string]git.HeadState{cwd: state}}
}

func buildEmptyTrees() *gittest.Trees {
	return &gittest.Trees{}
}

// makeCheckout is a directory that reads as a primary checkout.
func makeCheckout(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))

	return dir
}

func makeRemovedDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Remove(dir))

	return dir
}
