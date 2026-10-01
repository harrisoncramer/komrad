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
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/internal/waketest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncFixture is a real store with a workflow and the review store.
type syncFixture struct {
	stores     *storetest.Stores
	reviews    *reviewstore.ReviewStore
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
		before      agentstore.SetCheckoutParams
		cwd         func(t *testing.T) string
		head        git.HeadState
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
			before: agentstore.SetCheckoutParams{
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
			name:       "a checkout whose origin is not a forge clears what an earlier pass wrote",
			before:     agentstore.SetCheckoutParams{RepositoryURL: xptr.Ptr(komrad)},
			cwd:        makeCheckout,
			head:       git.HeadState{Branch: "main"},
			wantBranch: xptr.Ptr("main"),
		},
		{
			name: "a checkout records its branch, canonical address and commit over what an earlier pass wrote",
			before: agentstore.SetCheckoutParams{
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
			require.NoError(t, fixture.stores.Agents.SetCheckout(t.Context(), agent.ID, tt.before))

			require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, tt.head)}).Sync(t.Context()))

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
	syncer := fixture.newSyncer(NewCwdSyncerParams{Heads: heads, PullRequests: observations})

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, observations.passes, "recording a new branch observes")

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, observations.passes, "a branch already on the row observes nothing")

	heads.States[cwd] = git.HeadState{Branch: "agent/y"}
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 2, observations.passes, "moving branch observes again")
}

// TestCwdSyncerSyncWakesThePullRequestSyncOnAPush covers a pass waking the pull request sync only
// when origin's copy of a branch moved, and recording where it moved to.
func TestCwdSyncerSyncWakesThePullRequestSyncOnAPush(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	agent := fixture.seed(t, "a", cwd)

	heads := buildHeads(cwd, git.HeadState{Branch: "agent/x", SHA: "local"})
	wakes := &waketest.Counter{}
	syncer := fixture.newSyncer(NewCwdSyncerParams{Heads: heads, PullRequestSync: wakes})

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 0, wakes.Wakes, "a branch origin does not hold wakes nothing")

	heads.States[cwd] = git.HeadState{Branch: "agent/x", SHA: "local", PushedSHA: "local"}
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, wakes.Wakes, "a push wakes the sync")

	heads.States[cwd] = git.HeadState{Branch: "agent/x", SHA: "newer", PushedSHA: "local"}
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, wakes.Wakes, "a local commit wakes nothing")

	assert.Equal(t, xptr.Ptr("local"), fixture.read(t, agent.ID).PushedSHA)
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

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: heads}).Sync(t.Context()))

	agents, err := fixture.stores.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, count)
	for _, agent := range agents {
		require.NotNil(t, agent.Branch, agent.Name)
		assert.Equal(t, heads.States[agent.Cwd].Branch, *agent.Branch)
	}
}

// TestCwdSyncerSyncRecordsAStoppedSequence covers a rebase part-way through a checkout reaching the row.
func TestCwdSyncerSyncRecordsAStoppedSequence(t *testing.T) {
	fixture := newSyncFixture(t)
	repo := makeCheckout(t)
	agent := fixture.seed(t, "a", repo)

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(repo, git.HeadState{Branch: "topic", Sequence: git.SequenceMerge})}).Sync(t.Context()))

	got := fixture.read(t, agent.ID)
	assert.Equal(t, agentstore.SequenceMerge, got.Sequence)
	assert.Equal(t, xptr.Ptr("topic"), got.Branch)

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(repo, git.HeadState{Branch: "topic"})}).Sync(t.Context()))

	assert.Equal(t, agentstore.SequenceNone, fixture.read(t, agent.ID).Sequence)
}

// TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd covers a cwd that is gone never being asked for its branch.
func TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd(t *testing.T) {
	fixture := newSyncFixture(t)
	fixture.seed(t, "a", makeRemovedDir(t))
	looked := &gittest.Heads{}

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: looked}).Sync(t.Context()))

	assert.Empty(t, looked.Dirs)
}

// TestCwdSyncerSyncClearsWhatAnEarlierPassWrote covers a branch and a removal being cleared once the cwd moves.
func TestCwdSyncerSyncClearsWhatAnEarlierPassWrote(t *testing.T) {
	fixture := newSyncFixture(t)
	repo := makeCheckout(t)
	onBranch := fixture.seed(t, "on-branch", repo)
	removed := fixture.seed(t, "removed", makeRemovedDir(t))
	syncer := fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(repo, git.HeadState{Branch: "main"})})

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, xptr.Ptr("main"), fixture.read(t, onBranch.ID).Branch)
	require.True(t, fixture.read(t, removed.ID).WorktreeRemoved)

	for _, id := range []string{onBranch.ID, removed.ID} {
		agent := fixture.read(t, id)
		agent.Cwd = t.TempDir()
		require.NoError(t, fixture.stores.Agents.Update(t.Context(), *agent))
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
	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: heads}).Sync(t.Context()))

	for _, id := range ids {
		got := fixture.read(t, id)
		assert.Nil(t, got.Branch)
		assert.False(t, got.WorktreeRemoved)
	}
}

// TestCwdSyncerSyncRecordsAMoveSinceTheNewestRound covers the newest round being written once the tree no longer hashes to it.
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
			require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: heads, Trees: trees}).Sync(t.Context()))

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
	syncer := fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd, SnapshotTree: "tree-one"}), Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, "snapshot-one", fixture.read(t, agent.ID).MovedSinceRound)

	trees.ProbeErr = errors.New("the index could not be read")
	err := syncer.Sync(t.Context())

	require.ErrorContains(t, err, `failed to hash the worktree of "a"`)
	assert.Equal(t, "snapshot-one", fixture.read(t, agent.ID).MovedSinceRound)
}

// TestCwdSyncerSyncRecordsTheDiffStat covers the lines a pass measures against the workflow's base.
func TestCwdSyncerSyncRecordsTheDiffStat(t *testing.T) {
	tests := []struct {
		name         string
		cwd          func(t *testing.T) string
		diffStats    func(cwd string) map[string]git.DiffStat
		probe        string
		want         *agentstore.DiffStat
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
			want:         &agentstore.DiffStat{Added: 12, Removed: 3},
			wantMeasured: true,
		},
		{
			name:  "a checkout with a round measures the tree its probe already hashed",
			cwd:   makeCheckout,
			probe: "probed-tree",
			diffStats: func(cwd string) map[string]git.DiffStat {
				return map[string]git.DiffStat{cwd: {Added: 12, Removed: 3}}
			},
			want:         &agentstore.DiffStat{Added: 12, Removed: 3},
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
			require.NoError(t, fixture.stores.Agents.SetDiffStat(t.Context(), agent.ID, &agentstore.DiffStat{Added: 1, Removed: 1}))
			trees := &gittest.Trees{DiffStats: tt.diffStats(cwd)}
			head := git.HeadState{Root: cwd}
			if tt.probe != "" {
				trees.Probes = map[string]string{cwd: tt.probe}
				head.SnapshotTree = "round-tree"
			}

			require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, head), Trees: trees}).Sync(t.Context()))

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
	syncer := fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd}), Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, &agentstore.DiffStat{Added: 7, Removed: 2}, fixture.read(t, agent.ID).DiffStat)

	trees.DiffErr = errors.New("the index could not be read")
	err := syncer.Sync(t.Context())

	require.ErrorContains(t, err, `failed to measure the checkout of "a"`)
	assert.Equal(t, &agentstore.DiffStat{Added: 7, Removed: 2}, fixture.read(t, agent.ID).DiffStat)
}

// TestCwdSyncerSyncLandsTheRoundsTheBaseHolds covers the walk from the newest round down to the
// first one the base holds whole.
func TestCwdSyncerSyncLandsTheRoundsTheBaseHolds(t *testing.T) {
	tests := []struct {
		name         string
		tips         bool
		landed       map[string]bool
		wantWalked   []string
		wantUnlanded []int32
	}{
		{
			name:         "a base the clone does not have walks nothing",
			landed:       map[string]bool{"snapshot-three": true},
			wantUnlanded: []int32{3, 2, 1},
		},
		{
			name:         "a base holding no round walks them all and lands none",
			tips:         true,
			landed:       map[string]bool{},
			wantWalked:   []string{"snapshot-three", "snapshot-two", "snapshot-one"},
			wantUnlanded: []int32{3, 2, 1},
		},
		{
			name:         "a base holding a middle round lands it and every earlier one",
			tips:         true,
			landed:       map[string]bool{"snapshot-two": true},
			wantWalked:   []string{"snapshot-three", "snapshot-two"},
			wantUnlanded: []int32{3},
		},
		{
			name:         "a base holding the newest round lands them all",
			tips:         true,
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
			if tt.tips {
				trees.Tips = map[string]string{cwd: "tip-one"}
			}

			require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd}), Trees: trees}).Sync(t.Context()))

			assert.Equal(t, tt.wantWalked, trees.Walked)
			assert.Equal(t, tt.wantUnlanded, fixture.readUnlanded(t, agent.ID))
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
	syncer := fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd}), Trees: trees})

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
	assert.Empty(t, fixture.readUnlanded(t, agent.ID))
}

// TestCwdSyncerSyncFetchesTheBaseOncePerMerge covers the base being fetched when the agent's pull
// request merges, and not again until another does.
func TestCwdSyncerSyncFetchesTheBaseOncePerMerge(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	agent := fixture.seed(t, "a", cwd)
	fixture.release(t, agent.ID, "snapshot-one")
	trees := buildEmptyTrees()
	trees.Tips = map[string]string{cwd: "tip-one"}
	syncer := fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd}), Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Empty(t, trees.Fetched, "an open pull request fetches nothing")

	fixture.merge(t, agent.ID, time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	require.NoError(t, syncer.Sync(t.Context()))
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, []string{cwd}, trees.Fetched, "one merge is one fetch")
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()

	stores := storetest.Open(t)

	return &syncFixture{
		stores:     stores,
		reviews:    reviewstore.NewReviewStore(stores.Pool.GetDB()),
		workflowID: stores.SeedWorkflow(t, workflowstore.Config{Name: "flow"}).ID,
	}
}

func (f *syncFixture) seed(t *testing.T, name, cwd string) *agentstore.Agent {
	t.Helper()

	return f.stores.SeedAgent(t, agentstore.Agent{
		Name:       name,
		WorkflowID: f.workflowID,
		Cwd:        cwd,
	})
}

func (f *syncFixture) read(t *testing.T, id string) *agentstore.Agent {
	t.Helper()

	agent, err := f.stores.Agents.GetByID(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, agent)

	return agent
}

// release releases a review of a new round on the snapshot.
func (f *syncFixture) release(t *testing.T, agentID, snapshot string) {
	t.Helper()

	round := f.openRound(t, agentID, snapshot)
	review, err := f.reviews.OpenReview(t.Context(), reviewstore.OpenReviewParams{
		AgentID:    agentID,
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
	require.NoError(t, f.stores.Agents.SetCheckout(ctx, agentID, agentstore.SetCheckoutParams{
		RepositoryURL: &repository,
		Branch:        &branch,
	}))
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
		AgentID:     agentID,
		BaseSHA:     gittest.BaseSHA,
		SnapshotSHA: snapshot,
	})
	require.NoError(t, err)

	return round
}

// newSyncer builds a syncer over the real stores, filling in whatever params leaves out.
func (f *syncFixture) newSyncer(params NewCwdSyncerParams) *CwdSyncer {
	if params.Agents == nil {
		params.Agents = f.stores.Agents
	}
	if params.Heads == nil {
		params.Heads = &gittest.Heads{States: map[string]git.HeadState{}}
	}
	if params.Trees == nil {
		params.Trees = buildEmptyTrees()
	}
	if params.PullRequests == nil {
		params.PullRequests = &countingPullRequestObserver{}
	}
	if params.PullRequestSync == nil {
		params.PullRequestSync = &waketest.Counter{}
	}
	params.Rounds = f.reviews
	params.Workflows = f.stores.Workflows

	return NewCwdSyncer(params)
}

// readUnlanded is the ordinals of the agent's rounds that have not landed, newest first.
func (f *syncFixture) readUnlanded(t *testing.T, id string) []int32 {
	t.Helper()

	unlanded, err := f.reviews.ListUnlandedRounds(t.Context())
	require.NoError(t, err)

	return xslice.Map(unlanded[id], func(round reviewstore.UnlandedRound) int32 {
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
