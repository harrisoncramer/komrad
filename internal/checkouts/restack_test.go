package checkouts

import (
	"encoding/json"
	"errors"
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	"komrad/internal/storetest"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// taskOnParent is a task standing on a parent, as the restack tests lay it.
type taskOnParent struct {
	writer *agentstore.Agent
	task   string
	parent *taskstore.Task
}

// TestRestack covers one pass of the restack loop over a task standing on another.
func TestRestack(t *testing.T) {
	tests := []struct {
		name string
		// writer is the task's writer as it is seeded.
		writer agentstore.Agent
		// lay writes what the case needs beyond the task and its parent.
		lay func(t *testing.T, fixture *rebaseFixture, laid taskOnParent)
		// mover is the git of the task's checkout, told the parent's branch.
		mover func(parentBranch string) *gittest.Mover
		// wantStand is the stand read back after the pass, nil once the task is on the base branch.
		wantStand *taskstore.Stand
		wantCalls []string
		// wantPush is whether komrad force-pushed the branch.
		wantPush bool
		wantMail string
	}{
		{
			name: "a parent whose tip has not moved leaves the task alone",
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "oldtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name: "a parent not yet on origin leaves the task alone",
			mover: func(string) *gittest.Mover {
				return newChildGit("task/elsewhere", "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name:   "a writer part way through a turn is left to finish it",
			writer: agentstore.Agent{Pane: "%1", State: agentstore.StateThinking},
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name:   "a writer part way through a turn on a branch holding neither tip is not marked lost",
			writer: agentstore.Agent{Pane: "%1", State: agentstore.StateThinking},
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = nil

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name: "a worktree with changes not committed is left alone",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.State.TrackedChanges = true

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name: "a branch holding neither tip is marked lost and the writer told",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = nil

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "oldtip", Blocked: taskstore.RestackLost},
			wantMail:  "Your branch no longer holds oldtip, the commit of parent your own commits sat on",
		},
		{
			name: "a branch that lost the tip but holds the base, with no move onto the base, is marked lost",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = []string{"basesha"}

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "oldtip", Blocked: taskstore.RestackLost},
			wantMail:  "Your branch no longer holds oldtip, the commit of parent your own commits sat on",
		},
		{
			name: "a move onto the base still stopped on conflicts is left alone",
			lay:  seedStoppedMoveOntoBase,
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.State.InProgress = true
				mover.Holds = []string{"basesha"}

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name: "a move onto the base the writer aborted keeps the stand",
			lay:  seedStoppedMoveOntoBase,
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "oldtip")
				mover.Holds = []string{"oldtip", "basesha"}

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name: "a replay that conflicts is backed out and the writer told the rebase to run",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.ReplayErr = git.ErrRebaseConflicted
				mover.Prediction = git.Prediction{ConflictPaths: []string{"greet.go"}}

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "oldtip", Blocked: taskstore.RestackConflicted},
			wantCalls: []string{"park", "rebaseOnto", "abort rebase"},
			wantMail:  "conflicts in greet.go. Your branch is as it was. Run\ngit rebase --onto newtip oldtip",
		},
		{
			name: "a task whose replay conflicted is not replayed again",
			lay: func(t *testing.T, fixture *rebaseFixture, laid taskOnParent) {
				require.NoError(t, fixture.stores.Tasks.SetRestackBlocked(t.Context(), laid.task, taskstore.RestackConflicted))
			},
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip", Blocked: taskstore.RestackConflicted},
		},
		{
			name: "a parent whose pull request closed without merging marks the task and tells the writer",
			lay: func(t *testing.T, fixture *rebaseFixture, laid taskOnParent) {
				seedParentPullRequest(t, fixture, laid, "CLOSED")
			},
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip", Blocked: taskstore.RestackParentClosed},
			wantMail:  "The pull request of parent, the task yours stands on, closed without merging",
		},
		{
			name: "a restacked branch origin refuses is left for the writer to push",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Remote = git.RemoteStanding{Pushed: true}
				mover.PushErr = errors.New("stale info")

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "newtip"},
			wantCalls: []string{"park", "rebaseOnto"},
			wantPush:  true,
			wantMail:  "Komrad replayed your 2 commits onto it.",
		},
		{
			name: "a task whose replay conflicted has the parent's tip recorded once its branch holds it",
			lay: func(t *testing.T, fixture *rebaseFixture, laid taskOnParent) {
				require.NoError(t, fixture.stores.Tasks.SetRestackBlocked(t.Context(), laid.task, taskstore.RestackConflicted))
			},
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = []string{"newtip"}

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "newtip"},
		},
		{
			name: "a branch already holding the parent's tip records it without a replay",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = append(mover.Holds, "newtip")

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "newtip"},
		},
		{
			name: "a move onto the base the writer finished ends the stand while the parent stays put",
			lay:  seedStoppedMoveOntoBase,
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "oldtip")
				mover.Holds = []string{"basesha"}

				return mover
			},
		},
		{
			name: "a parent whose pull request merged has the task moved onto the base branch",
			lay: func(t *testing.T, fixture *rebaseFixture, laid taskOnParent) {
				seedParentPullRequest(t, fixture, laid, "MERGED")
			},
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantCalls: []string{"park", "rebaseOnto"},
			wantMail:  "parent merged into main. Komrad replayed your 2 commits onto main. Point your pull request at main.",
		},
		{
			name: "a restacked branch origin already holds is force-pushed",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Remote = git.RemoteStanding{Pushed: true}

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "newtip"},
			wantCalls: []string{"park", "rebaseOnto"},
			wantPush:  true,
			wantMail:  "onto it and pushed.",
		},
		{
			name: "a parent whose tip moved has the task's own commits replayed onto it",
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "newtip"},
			wantCalls: []string{"park", "rebaseOnto"},
			wantMail:  "parent moved to newtip. Komrad replayed your 2 commits onto it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			laid := fixture.seedTaskOnParent(t, tt.writer)
			if tt.lay != nil {
				tt.lay(t, fixture, laid)
			}
			mover := tt.mover(laid.parent.GetBranch())

			require.NoError(t, fixture.newService(NewServiceParams{Git: mover}).Restack(t.Context()))

			got, err := fixture.stores.Tasks.Get(t.Context(), laid.task)
			require.NoError(t, err)
			if tt.wantStand != nil {
				tt.wantStand.ParentID = laid.parent.ID
				tt.wantStand.PullRequestID = xptr.Deref(findPullRequestID(t, fixture, laid))
			}
			assert.Equal(t, tt.wantStand, got.Stand)
			assert.Subset(t, mover.Calls, tt.wantCalls)
			if tt.wantCalls == nil {
				assert.NotContains(t, mover.Calls, "rebaseOnto", "nothing is replayed")
			}
			assert.Equal(t, tt.wantPush, len(mover.Pushed) == 1 && mover.Pushed[0].Force)

			inbox := fixture.readInbox(t, laid.writer.ID)
			if tt.wantMail == "" {
				assert.Empty(t, inbox)

				return
			}
			require.Len(t, inbox, 1)
			assert.Contains(t, inbox[0], tt.wantMail)
		})
	}
}

// TestRestackMarksLostABranchThatAbortedAMoveOntoBase covers a writer aborting a stopped move onto the base
// branch, and the branch losing its tip on a later pass.
func TestRestackMarksLostABranchThatAbortedAMoveOntoBase(t *testing.T) {
	fixture := newRebaseFixture(t)
	laid := fixture.seedTaskOnParent(t, agentstore.Agent{})
	seedStoppedMoveOntoBase(t, fixture, laid)
	mover := newChildGit(laid.parent.GetBranch(), "oldtip")
	mover.Holds = []string{"oldtip", "basesha"}
	service := fixture.newService(NewServiceParams{Git: mover})

	require.NoError(t, service.Restack(t.Context()))

	mover.Tips[laid.parent.GetBranch()] = "newtip"
	mover.Holds = []string{"basesha"}

	require.NoError(t, service.Restack(t.Context()))

	got, err := fixture.stores.Tasks.Get(t.Context(), laid.task)
	require.NoError(t, err)
	require.NotNil(t, got.Stand)
	assert.Equal(t, taskstore.RestackLost, got.Stand.Blocked)
}

// TestRestackFoldsATaskWhosePullRequestMergedIntoItsParent covers a task merged into its
// parent's branch handing the tasks standing on it to that parent.
func TestRestackFoldsATaskWhosePullRequestMergedIntoItsParent(t *testing.T) {
	fixture := newRebaseFixture(t)
	ctx := t.Context()
	laid := fixture.seedTaskOnParent(t, agentstore.Agent{})
	merged, err := fixture.stores.Tasks.Get(ctx, laid.task)
	require.NoError(t, err)
	child := fixture.stores.SeedTask(t, taskstore.CreateParams{
		WorkflowID: fixture.workflowID,
		Slug:       "child",
		Stand:      &taskstore.Stand{ParentID: merged.ID, Tip: "mergedtip"},
	})
	fixture.stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
		RepositoryURL: pushRepository,
		Number:        1,
		HeadBranch:    merged.GetBranch(),
		HeadSHA:       "mergedtip",
		BaseBranch:    laid.parent.GetBranch(),
		State:         "MERGED",
	})
	mover := newChildGit(laid.parent.GetBranch(), "newtip")

	require.NoError(t, fixture.newService(NewServiceParams{Git: mover}).Restack(ctx))

	folded, err := fixture.stores.Tasks.Get(ctx, merged.ID)
	require.NoError(t, err)
	assert.Nil(t, folded.Stand, "the merged task stands on nothing")

	moved, err := fixture.stores.Tasks.Get(ctx, child.ID)
	require.NoError(t, err)
	assert.Equal(t, &taskstore.Stand{ParentID: laid.parent.ID, Tip: "mergedtip"}, moved.Stand)
	assert.NotContains(t, mover.Calls, "rebaseOnto")
}

// TestPlaceStand covers naming the commit a standing task's own commits sit on.
func TestPlaceStand(t *testing.T) {
	tests := []struct {
		name    string
		stands  bool
		holds   []string
		wantErr error
		wantTip string
	}{
		{
			name:    "a task standing on no other is refused",
			holds:   []string{"placed"},
			wantErr: errStandsOnNone,
		},
		{
			name:    "a commit the branch does not hold is refused",
			stands:  true,
			wantErr: errStandNotHeld,
			wantTip: "oldtip",
		},
		{
			name:    "a commit the branch holds becomes the tip",
			stands:  true,
			holds:   []string{"placed"},
			wantTip: "placed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{})
			if tt.stands {
				parent := fixture.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: fixture.workflowID, Slug: "parent"})
				require.NoError(t, fixture.stores.Tasks.SetStand(t.Context(), *agent.TaskID, &taskstore.Stand{ParentID: parent.ID, Tip: "oldtip"}))
			}
			mover := &gittest.Mover{Holds: tt.holds}

			sha, err := fixture.newService(NewServiceParams{Git: mover}).PlaceStand(t.Context(), PlaceStandParams{Task: *agent.TaskID, Commit: "placed"})

			got, readErr := fixture.stores.Tasks.Get(t.Context(), *agent.TaskID)
			require.NoError(t, readErr)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "placed", sha)
				assert.Equal(t, 1, fixture.checkoutSync.Wakes)
			}
			if tt.wantTip != "" {
				assert.Equal(t, tt.wantTip, got.Stand.Tip)
			}
		})
	}
}

// seedTaskOnParent records a writer on a task standing on a parent at oldtip, on a checkout that pushes to the test repository.
func (f *rebaseFixture) seedTaskOnParent(t *testing.T, writer agentstore.Agent) taskOnParent {
	t.Helper()

	parent := f.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: f.workflowID, Slug: "parent"})
	writer.Role = agentstore.RoleWriter
	seeded := f.seedPushable(t, writer, "")
	require.NoError(t, f.stores.Tasks.SetStand(t.Context(), *seeded.TaskID, &taskstore.Stand{ParentID: parent.ID, Tip: "oldtip"}))

	return taskOnParent{writer: seeded, task: *seeded.TaskID, parent: parent}
}

// findPullRequestID is the parent's pull request the task follows, nil while it follows none.
func findPullRequestID(t *testing.T, fixture *rebaseFixture, laid taskOnParent) *string {
	t.Helper()

	found, err := fixture.stores.PullRequests.GetStandPullRequest(t.Context(), pullrequeststore.GetStandPullRequestParams{
		RepositoryURL: pushRepository,
		HeadBranch:    laid.parent.GetBranch(),
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	require.NoError(t, err)

	return &found.ID
}

// seedParentPullRequest stores a pull request from the parent's branch onto main at newtip, in the state named.
func seedParentPullRequest(t *testing.T, fixture *rebaseFixture, laid taskOnParent, state string) {
	t.Helper()

	fixture.stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
		RepositoryURL: pushRepository,
		Number:        1,
		HeadBranch:    laid.parent.GetBranch(),
		HeadSHA:       "newtip",
		BaseBranch:    "main",
		State:         state,
	})
}

// seedStoppedMoveOntoBase records a move of the task onto the base branch at basesha that stopped on conflicts.
func seedStoppedMoveOntoBase(t *testing.T, fixture *rebaseFixture, laid taskOnParent) {
	t.Helper()

	raw, err := json.Marshal(rebaseOutcome{Result: &RebaseResult{
		RebasePlan: RebasePlan{BaseRef: "origin/main", BaseSHA: "basesha"},
		Conflicted: true,
		OntoBase:   true,
	}})
	require.NoError(t, err)
	require.NoError(t, fixture.stores.Tasks.SetLastRebase(t.Context(), laid.task, raw))
}

// newChildGit is the git of a clean checkout on oldtip, with the parent's branch on origin at tip.
func newChildGit(parentBranch, tip string) *gittest.Mover {
	return &gittest.Mover{
		State:   git.TreeState{Branch: "agent/a1"},
		Holds:   []string{"oldtip"},
		Tips:    map[string]string{parentBranch: tip},
		Commits: 2,
		SHA:     "pushedsha",
	}
}
