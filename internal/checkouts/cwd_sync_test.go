package checkouts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
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

// countingCwdSetter is the real agent store with its standing writes counted.
type countingCwdSetter struct {
	cwdSetter
	standingWrites int
}

func (c *countingCwdSetter) SetTreeStanding(ctx context.Context, id string, standing git.Standing, snapshotSHA string) error {
	c.standingWrites++

	return c.cwdSetter.SetTreeStanding(ctx, id, standing, snapshotSHA)
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

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(repo, git.HeadState{Branch: "topic", SequenceInProgress: true})}).Sync(t.Context()))

	got := fixture.read(t, agent.ID)
	assert.True(t, got.SequenceInProgress)
	assert.Equal(t, xptr.Ptr("topic"), got.Branch)

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(repo, git.HeadState{Branch: "topic"})}).Sync(t.Context()))

	assert.False(t, fixture.read(t, agent.ID).SequenceInProgress)
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

// TestCwdSyncerSyncWritesAStandingOnlyWhenItMoved covers a pass writing a standing only where the row differs.
func TestCwdSyncerSyncWritesAStandingOnlyWhenItMoved(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	agent := fixture.seed(t, "a", cwd)
	fixture.release(t, agent.ID, "snapshot-one", reviewstore.VerdictApproved)

	trees := &gittest.Trees{Standings: map[string]git.Standing{cwd: git.StandingSame}}
	agents := &countingCwdSetter{cwdSetter: fixture.stores.Agents}
	syncer := fixture.newSyncer(NewCwdSyncerParams{Agents: agents, Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, agents.standingWrites, "the first pass records the round the tree was measured against")
	assert.Equal(t, "snapshot-one", fixture.read(t, agent.ID).TreeStandingSHA)

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, agents.standingWrites, "a pass that finds the tree where it was writes nothing")

	trees.Standings[cwd] = git.StandingMoved
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 2, agents.standingWrites, "a tree that moved against its round is written")
	assert.Equal(t, git.StandingMoved, fixture.readRecordedStanding(t, agent.ID, "snapshot-one"))

	trees.Standings[cwd] = git.StandingSame
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, git.StandingSame, fixture.readRecordedStanding(t, agent.ID, "snapshot-one"), "a move the next pass does not see is forgotten")
}

// TestCwdSyncerSyncReadsWhereTheTreeStands covers each standing git reports against the last verdict's round.
func TestCwdSyncerSyncReadsWhereTheTreeStands(t *testing.T) {
	fixture := newSyncFixture(t)
	standings := map[string]git.Standing{
		"edited":        git.StandingMoved,
		"untouched":     git.StandingSame,
		"landed":        git.StandingLanded,
		"landed-edited": git.StandingLandedMoved,
	}
	trees := &gittest.Trees{Standings: map[string]git.Standing{}}
	ids := map[string]string{}
	for name, standing := range standings {
		cwd := makeCheckout(t)
		trees.Standings[cwd] = standing
		ids[name] = fixture.seed(t, name, cwd).ID
		fixture.release(t, ids[name], "snapshot-"+name, reviewstore.VerdictApproved)
	}

	assert.Equal(t, git.StandingSame, fixture.readRecordedStanding(t, ids["edited"], "snapshot-edited"), "nothing is recorded before the first pass")

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Trees: trees}).Sync(t.Context()))

	for name, standing := range standings {
		assert.Equal(t, standing, fixture.readRecordedStanding(t, ids[name], "snapshot-"+name), name)
	}
}

// TestCwdSyncerSyncReportsATreeItCouldNotRead covers a failed tree read being returned rather than read as unmoved.
func TestCwdSyncerSyncReportsATreeItCouldNotRead(t *testing.T) {
	tests := []struct {
		name         string
		trees        *gittest.Trees
		wantErr      string
		wantStanding git.Standing
	}{
		{
			name:         "a tree the pass cannot compare",
			trees:        buildEmptyTrees(),
			wantErr:      `failed to compare the tree of "a"`,
			wantStanding: git.StandingSame,
		},
		{
			name: "a failure after the tree was found to have moved",
			trees: &gittest.Trees{
				AllMoved:    true,
				StandingErr: errors.New("the base could not be merged"),
			},
			wantErr:      "the base could not be merged",
			wantStanding: git.StandingMoved,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSyncFixture(t)
			agent := fixture.seed(t, "a", makeCheckout(t))
			fixture.release(t, agent.ID, "snapshot-one", reviewstore.VerdictApproved)

			err := fixture.newSyncer(NewCwdSyncerParams{Trees: tt.trees}).Sync(t.Context())

			require.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, tt.wantStanding, fixture.readRecordedStanding(t, agent.ID, "snapshot-one"))
		})
	}
}

// TestCwdSyncerSyncMeasuresAgainstTheWorkflowsBase covers the landed question being asked of the workflow's base.
func TestCwdSyncerSyncMeasuresAgainstTheWorkflowsBase(t *testing.T) {
	fixture := newSyncFixture(t)
	develop := fixture.stores.SeedWorkflow(t, workflowstore.Config{Name: "develop", Worktree: workflowstore.WorktreeConfig{BaseBranch: "develop"}})
	agent := fixture.stores.SeedAgent(t, agentstore.Agent{
		Name:       "reviewed",
		WorkflowID: develop.ID,
		Cwd:        makeCheckout(t),
	})
	fixture.release(t, agent.ID, "snapshot-one", reviewstore.VerdictApproved)
	hashed := &gittest.Trees{AllMoved: true}

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Trees: hashed}).Sync(t.Context()))

	assert.Equal(t, []string{"develop"}, hashed.Bases)
}

// TestCwdSyncerSyncHashesOnlyAReviewedTree covers only a reviewed tree still on disk being hashed.
func TestCwdSyncerSyncHashesOnlyAReviewedTree(t *testing.T) {
	fixture := newSyncFixture(t)
	reviewed := makeCheckout(t)
	reviewedAgent := fixture.seed(t, "reviewed", reviewed)
	fixture.seed(t, "unreviewed", makeCheckout(t))
	commentedAgent := fixture.seed(t, "commented", makeCheckout(t))
	removedAgent := fixture.seed(t, "removed", makeRemovedDir(t))
	fixture.release(t, reviewedAgent.ID, "snapshot-one", reviewstore.VerdictApproved)
	fixture.release(t, commentedAgent.ID, "snapshot-two", reviewstore.VerdictCommented)
	fixture.release(t, removedAgent.ID, "snapshot-three", reviewstore.VerdictApproved)
	hashed := &gittest.Trees{AllMoved: true}

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Trees: hashed}).Sync(t.Context()))

	assert.Equal(t, []string{reviewed}, hashed.Dirs)
	assert.Equal(t, git.StandingMoved, fixture.readRecordedStanding(t, reviewedAgent.ID, "snapshot-one"))
	assert.Equal(t, git.StandingSame, fixture.readRecordedStanding(t, removedAgent.ID, "snapshot-three"))
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
			trees := &gittest.Trees{AllMoved: true, Probes: map[string]string{cwd: tt.probe}}
			require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: heads, Trees: trees}).Sync(t.Context()))

			assert.Equal(t, tt.want, fixture.read(t, agent.ID).MovedSinceRound)
		})
	}
}

// TestCwdSyncerSyncHashesTheWorktreeOnceForBothRounds covers one probe serving the concluded and the newest round.
func TestCwdSyncerSyncHashesTheWorktreeOnceForBothRounds(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	agent := fixture.seed(t, "a", cwd)
	fixture.release(t, agent.ID, "snapshot-one", reviewstore.VerdictApproved)
	fixture.openRound(t, agent.ID, "snapshot-two")
	trees := &gittest.Trees{AllMoved: true, Probes: map[string]string{cwd: "tree-moved"}}

	require.NoError(t, fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd, SnapshotTree: "tree-two"}), Trees: trees}).Sync(t.Context()))

	assert.Equal(t, []string{cwd}, trees.Probed, "the worktree is hashed once")
	assert.Equal(t, []string{"tree-moved"}, trees.Given, "and the standing reads that hash")
	assert.Equal(t, "snapshot-two", fixture.read(t, agent.ID).MovedSinceRound)
	assert.Equal(t, git.StandingMoved, fixture.readRecordedStanding(t, agent.ID, "snapshot-one"))
}

// TestCwdSyncerSyncKeepsAMoveItCouldNotProbe covers a failed probe leaving the recorded move alone.
func TestCwdSyncerSyncKeepsAMoveItCouldNotProbe(t *testing.T) {
	fixture := newSyncFixture(t)
	cwd := makeCheckout(t)
	agent := fixture.seed(t, "a", cwd)
	fixture.openRound(t, agent.ID, "snapshot-one")
	trees := &gittest.Trees{AllMoved: true, Probes: map[string]string{cwd: "tree-moved"}}
	syncer := fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd, SnapshotTree: "tree-one"}), Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, "snapshot-one", fixture.read(t, agent.ID).MovedSinceRound)

	trees.ProbeErr = errors.New("the index could not be read")
	err := syncer.Sync(t.Context())

	require.ErrorContains(t, err, `failed to hash the worktree of "a"`)
	assert.Equal(t, "snapshot-one", fixture.read(t, agent.ID).MovedSinceRound)
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
				fixture.release(t, agent.ID, snapshot, reviewstore.VerdictCommented)
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
	fixture.release(t, agent.ID, "snapshot-one", reviewstore.VerdictCommented)
	trees := buildEmptyTrees()
	trees.Tips = map[string]string{cwd: "tip-one"}
	syncer := fixture.newSyncer(NewCwdSyncerParams{Heads: buildHeads(cwd, git.HeadState{Root: cwd}), Trees: trees})

	require.NoError(t, syncer.Sync(t.Context()))
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, []string{"snapshot-one"}, trees.Walked, "an unmoved base is not walked twice")

	fixture.release(t, agent.ID, "snapshot-two", reviewstore.VerdictCommented)
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, []string{"snapshot-one", "snapshot-two", "snapshot-one"}, trees.Walked, "a new round is walked")

	trees.Tips[cwd] = "tip-two"
	trees.Landed = map[string]bool{"snapshot-two": true}
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, []string{"snapshot-one", "snapshot-two", "snapshot-one", "snapshot-two"}, trees.Walked, "a moved base is walked")
	assert.Empty(t, fixture.readUnlanded(t, agent.ID))
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
func (f *syncFixture) release(t *testing.T, agentID, snapshot string, verdict reviewstore.Verdict) {
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
		Verdict: verdict,
	})
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
	params.Rounds = f.reviews
	params.Bases = f.stores.Workflows

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

// readRecordedStanding is the standing the row records against the round, unmoved against any other.
func (f *syncFixture) readRecordedStanding(t *testing.T, id, round string) git.Standing {
	t.Helper()

	agent := f.read(t, id)
	if agent.TreeStandingSHA != round {
		return git.StandingSame
	}

	return agent.TreeStanding
}

func buildHeads(cwd string, state git.HeadState) *gittest.Heads {
	return &gittest.Heads{States: map[string]git.HeadState{cwd: state}}
}

func buildEmptyTrees() *gittest.Trees {
	return &gittest.Trees{Standings: map[string]git.Standing{}}
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
