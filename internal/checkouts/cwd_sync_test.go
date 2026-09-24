package checkouts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/git"
	"weave/internal/gittest"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncFixture is a real store with a workflow and the review store.
type syncFixture struct {
	stores     *storetest.Stores
	reviews    *reviewstore.ReviewStore
	workflowID string
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

func (f *syncFixture) syncer(heads headReader, trees treeReader) *CwdSyncer {
	return NewCwdSyncer(NewCwdSyncerParams{
		Agents:       f.stores.Agents,
		Heads:        heads,
		Trees:        trees,
		Rounds:       f.reviews,
		Bases:        f.stores.Workflows,
		PullRequests: &countingObservations{},
	})
}

func headsAt(cwd string, state git.HeadState) *gittest.Heads {
	return &gittest.Heads{States: map[string]git.HeadState{cwd: state}}
}

func noTrees() *gittest.Trees {
	return &gittest.Trees{Standings: map[string]git.Standing{}}
}

// standingOn is the standing the row records against the round, unmoved against any other.
func (f *syncFixture) standingOn(t *testing.T, id, round string) git.Standing {
	t.Helper()

	agent := f.read(t, id)
	if agent.TreeStandingSHA != round {
		return git.StandingSame
	}

	return agent.TreeStanding
}

// TestCwdSyncerSyncRecordsTheCheckout covers what a pass writes of a live agent's checkout.
func TestCwdSyncerSyncRecordsTheCheckout(t *testing.T) {
	const weave = "https://github.com/harrisoncramer/weave"
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
			cwd:  func(*testing.T) string { return "" },
		},
		{
			name: "a directory that is not a checkout carries nothing",
			cwd:  func(t *testing.T) string { return t.TempDir() },
		},
		{
			name: "a cwd that no longer exists is reported removed and cleared",
			before: agentstore.SetCheckoutParams{
				Branch:        xptr.Ptr("main"),
				RepositoryURL: xptr.Ptr(weave),
				HeadSHA:       xptr.Ptr("aaa"),
			},
			cwd: removedDir,
			head: git.HeadState{
				Branch:        "main",
				RepositoryURL: weave,
				SHA:           "aaa",
			},
			wantRemoved: true,
		},
		{
			name:       "a checkout whose origin is not a forge clears what an earlier pass wrote",
			before:     agentstore.SetCheckoutParams{RepositoryURL: xptr.Ptr(weave)},
			cwd:        fakeCheckout,
			head:       git.HeadState{Branch: "main"},
			wantBranch: xptr.Ptr("main"),
		},
		{
			name: "a checkout records its branch, canonical address and commit",
			cwd:  fakeCheckout,
			head: git.HeadState{
				Branch:        "feature/x",
				RepositoryURL: weave,
				SHA:           "aaa",
			},
			wantBranch: xptr.Ptr("feature/x"),
			wantRepo:   xptr.Ptr(weave),
			wantSHA:    xptr.Ptr("aaa"),
		},
		{
			name: "an origin moved and a commit made under a running agent are rewritten",
			before: agentstore.SetCheckoutParams{
				Branch:        xptr.Ptr("main"),
				RepositoryURL: xptr.Ptr(weave),
				HeadSHA:       xptr.Ptr("aaa"),
			},
			cwd: fakeCheckout,
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
			f := newSyncFixture(t)
			cwd := tt.cwd(t)
			agent := f.seed(t, "a", cwd)
			require.NoError(t, f.stores.Agents.SetCheckout(t.Context(), agent.ID, tt.before))

			require.NoError(t, f.syncer(headsAt(cwd, tt.head), noTrees()).Sync(t.Context()))

			got := f.read(t, agent.ID)
			assert.Equal(t, tt.wantRemoved, got.WorktreeRemoved)
			assert.Equal(t, tt.wantBranch, got.Branch)
			assert.Equal(t, tt.wantRepo, got.RepositoryURL)
			assert.Equal(t, tt.wantSHA, got.HeadSHA)
		})
	}
}

// TestCwdSyncerSyncObservesANewBranch covers a pass observing pull requests only when a branch moved.
func TestCwdSyncerSyncObservesANewBranch(t *testing.T) {
	f := newSyncFixture(t)
	cwd := fakeCheckout(t)
	f.seed(t, "a", cwd)

	heads := headsAt(cwd, git.HeadState{Branch: "agent/x"})
	observations := &countingObservations{}
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents:       f.stores.Agents,
		Heads:        heads,
		Trees:        noTrees(),
		Rounds:       f.reviews,
		Bases:        f.stores.Workflows,
		PullRequests: observations,
	})

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
	f := newSyncFixture(t)
	heads := &gittest.Heads{States: map[string]git.HeadState{}}
	count := cwdLookWorkers*2 + 1
	for i := range count {
		cwd := fakeCheckout(t)
		heads.States[cwd] = git.HeadState{Branch: fmt.Sprintf("b%d", i)}
		f.seed(t, fmt.Sprintf("a%d", i), cwd)
	}

	require.NoError(t, f.syncer(heads, noTrees()).Sync(t.Context()))

	agents, err := f.stores.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, count)
	for _, agent := range agents {
		require.NotNil(t, agent.Branch, agent.Name)
		assert.Equal(t, heads.States[agent.Cwd].Branch, *agent.Branch)
	}
}

// TestCwdSyncerSyncRecordsAStoppedSequence covers a rebase part-way through a checkout reaching the row.
func TestCwdSyncerSyncRecordsAStoppedSequence(t *testing.T) {
	f := newSyncFixture(t)
	repo := fakeCheckout(t)
	agent := f.seed(t, "a", repo)

	require.NoError(t, f.syncer(headsAt(repo, git.HeadState{Branch: "topic", SequenceInProgress: true}), noTrees()).Sync(t.Context()))

	got := f.read(t, agent.ID)
	assert.True(t, got.SequenceInProgress)
	assert.Equal(t, xptr.Ptr("topic"), got.Branch)

	require.NoError(t, f.syncer(headsAt(repo, git.HeadState{Branch: "topic"}), noTrees()).Sync(t.Context()))

	assert.False(t, f.read(t, agent.ID).SequenceInProgress)
}

// TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd covers a cwd that is gone never being asked for its branch.
func TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd(t *testing.T) {
	f := newSyncFixture(t)
	f.seed(t, "a", removedDir(t))
	looked := &gittest.Heads{}

	require.NoError(t, f.syncer(looked, noTrees()).Sync(t.Context()))

	assert.Empty(t, looked.Dirs)
}

// TestCwdSyncerSyncClearsWhatAnEarlierPassWrote covers a branch and a removal being cleared once the cwd moves.
func TestCwdSyncerSyncClearsWhatAnEarlierPassWrote(t *testing.T) {
	f := newSyncFixture(t)
	repo := fakeCheckout(t)
	onBranch := f.seed(t, "on-branch", repo)
	removed := f.seed(t, "removed", removedDir(t))
	syncer := f.syncer(headsAt(repo, git.HeadState{Branch: "main"}), noTrees())

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, xptr.Ptr("main"), f.read(t, onBranch.ID).Branch)
	require.True(t, f.read(t, removed.ID).WorktreeRemoved)

	for _, id := range []string{onBranch.ID, removed.ID} {
		agent := f.read(t, id)
		agent.Cwd = t.TempDir()
		require.NoError(t, f.stores.Agents.Update(t.Context(), *agent))
	}

	require.NoError(t, syncer.Sync(t.Context()))

	assert.Nil(t, f.read(t, onBranch.ID).Branch)
	assert.False(t, f.read(t, removed.ID).WorktreeRemoved)
}

// TestCwdSyncerSyncSkipsArchivedAgents covers that the pass walks live agents only.
func TestCwdSyncerSyncSkipsArchivedAgents(t *testing.T) {
	f := newSyncFixture(t)
	checkout := fakeCheckout(t)
	gone := removedDir(t)
	ids := []string{f.seed(t, "checkout", checkout).ID, f.seed(t, "gone", gone).ID}
	_, err := f.stores.Agents.Archive(t.Context(), ids)
	require.NoError(t, err)

	heads := &gittest.Heads{States: map[string]git.HeadState{
		checkout: {Branch: "main"},
		gone:     {Branch: "main"},
	}}
	require.NoError(t, f.syncer(heads, noTrees()).Sync(t.Context()))

	for _, id := range ids {
		got := f.read(t, id)
		assert.Nil(t, got.Branch)
		assert.False(t, got.WorktreeRemoved)
	}
}

// countingAgents is the real agent store with its standing writes counted.
type countingAgents struct {
	cwdSetter
	standingWrites int
}

func (c *countingAgents) SetTreeStanding(ctx context.Context, id string, standing git.Standing, snapshotSHA string) error {
	c.standingWrites++

	return c.cwdSetter.SetTreeStanding(ctx, id, standing, snapshotSHA)
}

// TestCwdSyncerSyncWritesAStandingOnlyWhenItMoved covers a pass writing a standing only where the row differs.
func TestCwdSyncerSyncWritesAStandingOnlyWhenItMoved(t *testing.T) {
	f := newSyncFixture(t)
	cwd := fakeCheckout(t)
	agent := f.seed(t, "a", cwd)
	f.release(t, agent.ID, "snapshot-one", reviewstore.VerdictApproved)

	trees := &gittest.Trees{Standings: map[string]git.Standing{cwd: git.StandingSame}}
	agents := &countingAgents{cwdSetter: f.stores.Agents}
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents:       agents,
		Heads:        &gittest.Heads{States: map[string]git.HeadState{}},
		Trees:        trees,
		Rounds:       f.reviews,
		Bases:        f.stores.Workflows,
		PullRequests: &countingObservations{},
	})

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, agents.standingWrites, "the first pass records the round the tree was measured against")
	assert.Equal(t, "snapshot-one", f.read(t, agent.ID).TreeStandingSHA)

	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 1, agents.standingWrites, "a pass that finds the tree where it was writes nothing")

	trees.Standings[cwd] = git.StandingMoved
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, 2, agents.standingWrites, "a tree that moved against its round is written")
	assert.Equal(t, git.StandingMoved, f.standingOn(t, agent.ID, "snapshot-one"))

	trees.Standings[cwd] = git.StandingSame
	require.NoError(t, syncer.Sync(t.Context()))
	assert.Equal(t, git.StandingSame, f.standingOn(t, agent.ID, "snapshot-one"), "a move the next pass does not see is forgotten")
}

// TestCwdSyncerSyncReadsWhereTheTreeStands covers each standing git reports against the last verdict's round.
func TestCwdSyncerSyncReadsWhereTheTreeStands(t *testing.T) {
	f := newSyncFixture(t)
	standings := map[string]git.Standing{
		"edited":        git.StandingMoved,
		"untouched":     git.StandingSame,
		"landed":        git.StandingLanded,
		"landed-edited": git.StandingLandedMoved,
	}
	trees := &gittest.Trees{Standings: map[string]git.Standing{}}
	ids := map[string]string{}
	for name, standing := range standings {
		cwd := fakeCheckout(t)
		trees.Standings[cwd] = standing
		ids[name] = f.seed(t, name, cwd).ID
		f.release(t, ids[name], "snapshot-"+name, reviewstore.VerdictApproved)
	}

	assert.Equal(t, git.StandingSame, f.standingOn(t, ids["edited"], "snapshot-edited"), "nothing is recorded before the first pass")

	require.NoError(t, f.syncer(&gittest.Heads{States: map[string]git.HeadState{}}, trees).Sync(t.Context()))

	for name, standing := range standings {
		assert.Equal(t, standing, f.standingOn(t, ids[name], "snapshot-"+name), name)
	}
	assert.Equal(t, git.StandingSame, f.standingOn(t, ids["edited"], "snapshot-newer"),
		"a verdict on a round the pass has not compared against is not one the tree moved from")
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
			trees:        noTrees(),
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
			f := newSyncFixture(t)
			agent := f.seed(t, "a", fakeCheckout(t))
			f.release(t, agent.ID, "snapshot-one", reviewstore.VerdictApproved)

			err := f.syncer(&gittest.Heads{States: map[string]git.HeadState{}}, tt.trees).Sync(t.Context())

			require.ErrorContains(t, err, tt.wantErr)
			assert.Equal(t, tt.wantStanding, f.standingOn(t, agent.ID, "snapshot-one"))
		})
	}
}

// TestCwdSyncerSyncMeasuresAgainstTheWorkflowsBase covers the landed question being asked of the workflow's base.
func TestCwdSyncerSyncMeasuresAgainstTheWorkflowsBase(t *testing.T) {
	f := newSyncFixture(t)
	develop := f.stores.SeedWorkflow(t, workflowstore.Config{Name: "develop", Worktree: workflowstore.WorktreeConfig{BaseBranch: "develop"}})
	agent := f.stores.SeedAgent(t, agentstore.Agent{
		Name:       "reviewed",
		WorkflowID: develop.ID,
		Cwd:        fakeCheckout(t),
	})
	f.release(t, agent.ID, "snapshot-one", reviewstore.VerdictApproved)
	hashed := &gittest.Trees{AllMoved: true}

	require.NoError(t, f.syncer(&gittest.Heads{States: map[string]git.HeadState{}}, hashed).Sync(t.Context()))

	assert.Equal(t, []string{"develop"}, hashed.Bases)
}

// TestCwdSyncerSyncHashesOnlyAReviewedTree covers only a reviewed tree still on disk being hashed.
func TestCwdSyncerSyncHashesOnlyAReviewedTree(t *testing.T) {
	f := newSyncFixture(t)
	reviewed := fakeCheckout(t)
	reviewedAgent := f.seed(t, "reviewed", reviewed)
	f.seed(t, "unreviewed", fakeCheckout(t))
	commentedAgent := f.seed(t, "commented", fakeCheckout(t))
	removedAgent := f.seed(t, "removed", removedDir(t))
	f.release(t, reviewedAgent.ID, "snapshot-one", reviewstore.VerdictApproved)
	f.release(t, commentedAgent.ID, "snapshot-two", reviewstore.VerdictCommented)
	f.release(t, removedAgent.ID, "snapshot-three", reviewstore.VerdictApproved)
	hashed := &gittest.Trees{AllMoved: true}

	require.NoError(t, f.syncer(&gittest.Heads{States: map[string]git.HeadState{}}, hashed).Sync(t.Context()))

	assert.Equal(t, []string{reviewed}, hashed.Dirs)
	assert.Equal(t, git.StandingMoved, f.standingOn(t, reviewedAgent.ID, "snapshot-one"))
	assert.Equal(t, git.StandingSame, f.standingOn(t, removedAgent.ID, "snapshot-three"))
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
			f := newSyncFixture(t)
			cwd := fakeCheckout(t)
			agent := f.seed(t, "a", cwd)
			if tt.newest != "" {
				f.openRound(t, agent.ID, tt.newest)
			}

			heads := headsAt(cwd, git.HeadState{Root: cwd, SnapshotTree: tt.snapshotTree})
			trees := &gittest.Trees{AllMoved: true, Probes: map[string]string{cwd: tt.probe}}
			require.NoError(t, f.syncer(heads, trees).Sync(t.Context()))

			assert.Equal(t, tt.want, f.read(t, agent.ID).MovedSinceRound)
		})
	}
}

// TestCwdSyncerSyncHashesTheWorktreeOnceForBothRounds covers one probe serving the concluded and the newest round.
func TestCwdSyncerSyncHashesTheWorktreeOnceForBothRounds(t *testing.T) {
	f := newSyncFixture(t)
	cwd := fakeCheckout(t)
	agent := f.seed(t, "a", cwd)
	f.release(t, agent.ID, "snapshot-one", reviewstore.VerdictApproved)
	f.openRound(t, agent.ID, "snapshot-two")
	trees := &gittest.Trees{AllMoved: true, Probes: map[string]string{cwd: "tree-moved"}}

	require.NoError(t, f.syncer(headsAt(cwd, git.HeadState{Root: cwd, SnapshotTree: "tree-two"}), trees).Sync(t.Context()))

	assert.Equal(t, []string{cwd}, trees.Probed, "the worktree is hashed once")
	assert.Equal(t, []string{"tree-moved"}, trees.Given, "and the standing reads that hash")
	assert.Equal(t, "snapshot-two", f.read(t, agent.ID).MovedSinceRound)
	assert.Equal(t, git.StandingMoved, f.standingOn(t, agent.ID, "snapshot-one"))
}

// TestCwdSyncerSyncKeepsAMoveItCouldNotProbe covers a failed probe leaving the recorded move alone.
func TestCwdSyncerSyncKeepsAMoveItCouldNotProbe(t *testing.T) {
	f := newSyncFixture(t)
	cwd := fakeCheckout(t)
	agent := f.seed(t, "a", cwd)
	f.openRound(t, agent.ID, "snapshot-one")
	trees := &gittest.Trees{AllMoved: true, Probes: map[string]string{cwd: "tree-moved"}}
	syncer := f.syncer(headsAt(cwd, git.HeadState{Root: cwd, SnapshotTree: "tree-one"}), trees)

	require.NoError(t, syncer.Sync(t.Context()))
	require.Equal(t, "snapshot-one", f.read(t, agent.ID).MovedSinceRound)

	trees.ProbeErr = errors.New("the index could not be read")
	err := syncer.Sync(t.Context())

	require.ErrorContains(t, err, `failed to hash the worktree of "a"`)
	assert.Equal(t, "snapshot-one", f.read(t, agent.ID).MovedSinceRound)
}

// fakeCheckout is a directory that reads as a primary checkout.
func fakeCheckout(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))

	return dir
}

func removedDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Remove(dir))

	return dir
}

// countingObservations counts the observation passes it is asked for.
type countingObservations struct {
	passes int
}

func (c *countingObservations) Observe(context.Context) (int64, error) {
	c.passes++

	return 0, nil
}
