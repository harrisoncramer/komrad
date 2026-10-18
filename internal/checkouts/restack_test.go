package checkouts

import (
	"testing"
	"time"

	"komrad/internal/git"
	"komrad/internal/gittest"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// standing is a task standing on a parent, as the restack tests lay it.
type standing struct {
	writer *agentstore.Agent
	task   string
	parent *taskstore.Task
}

// TestRestack covers one pass of the restack loop over a task standing on another.
func TestRestack(t *testing.T) {
	tests := []struct {
		name string
		// writer is the standing task's writer as it is seeded.
		writer agentstore.Agent
		// lay writes what the case needs beyond the standing task and its parent.
		lay func(t *testing.T, fixture *rebaseFixture, laid standing)
		// mover is the git of the standing task's checkout, told the parent's branch.
		mover func(parentBranch string) *gittest.Mover
		// wantStand is the stand read back after the pass, nil once the task is on the base branch.
		wantStand *taskstore.Stand
		wantCalls []string
		wantPush  bool
		wantMail  string
	}{
		{
			name: "a parent whose tip moved has the task's own commits replayed onto it",
			mover: func(parentBranch string) *gittest.Mover {
				return newRestackMover(parentBranch, "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "newtip"},
			wantCalls: []string{"park", "rebaseOnto"},
			wantMail:  "parent moved to newtip. Komrad replayed your 2 commits onto it.",
		},
		{
			name: "a restacked branch origin already holds is force-pushed",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newRestackMover(parentBranch, "newtip")
				mover.Remote = git.RemoteStanding{Pushed: true}

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "newtip"},
			wantCalls: []string{"park", "rebaseOnto"},
			wantPush:  true,
			wantMail:  "onto it and pushed.",
		},
		{
			name: "a parent whose tip has not moved leaves the task alone",
			mover: func(parentBranch string) *gittest.Mover {
				return newRestackMover(parentBranch, "oldtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name: "a parent not yet on origin leaves the task alone",
			mover: func(string) *gittest.Mover {
				return newRestackMover("task/elsewhere", "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name: "a branch already holding the parent's tip records it without a replay",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newRestackMover(parentBranch, "newtip")
				mover.Holds = append(mover.Holds, "newtip")

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "newtip"},
		},
		{
			name: "a branch holding neither tip is marked lost",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newRestackMover(parentBranch, "newtip")
				mover.Holds = nil

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "oldtip", Blocked: taskstore.RestackLost},
		},
		{
			name:   "a writer part way through a turn is left to finish it",
			writer: agentstore.Agent{Pane: "%1", State: agentstore.StateThinking},
			mover: func(parentBranch string) *gittest.Mover {
				return newRestackMover(parentBranch, "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name: "a worktree with changes not committed is left alone",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newRestackMover(parentBranch, "newtip")
				mover.State.TrackedChanges = true

				return mover
			},
			wantStand: &taskstore.Stand{Tip: "oldtip"},
		},
		{
			name: "a replay that conflicts is backed out and the writer told the rebase to run",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newRestackMover(parentBranch, "newtip")
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
			lay: func(t *testing.T, fixture *rebaseFixture, laid standing) {
				require.NoError(t, fixture.stores.Tasks.SetRestackBlocked(t.Context(), laid.task, taskstore.RestackConflicted))
			},
			mover: func(parentBranch string) *gittest.Mover {
				return newRestackMover(parentBranch, "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip", Blocked: taskstore.RestackConflicted},
		},
		{
			name: "a parent whose pull request merged has the task moved onto the base branch",
			lay: func(t *testing.T, fixture *rebaseFixture, laid standing) {
				fixture.seedStandPullRequest(t, laid.parent.BranchName(), "MERGED")
			},
			mover: func(parentBranch string) *gittest.Mover {
				return newRestackMover(parentBranch, "newtip")
			},
			wantCalls: []string{"park", "rebaseOnto"},
			wantMail:  "parent merged into main. Komrad replayed your 2 commits onto main. Point your pull request at main.",
		},
		{
			name: "a parent whose pull request closed without merging marks the task and tells the writer",
			lay: func(t *testing.T, fixture *rebaseFixture, laid standing) {
				fixture.seedStandPullRequest(t, laid.parent.BranchName(), "CLOSED")
			},
			mover: func(parentBranch string) *gittest.Mover {
				return newRestackMover(parentBranch, "newtip")
			},
			wantStand: &taskstore.Stand{Tip: "oldtip", Blocked: taskstore.RestackParentClosed},
			wantMail:  "The pull request of parent, the task yours stands on, closed without merging",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			laid := fixture.seedStanding(t, tt.writer)
			if tt.lay != nil {
				tt.lay(t, fixture, laid)
			}
			mover := tt.mover(laid.parent.BranchName())

			require.NoError(t, fixture.newService(NewServiceParams{Git: mover}).Restack(t.Context()))

			got, err := fixture.stores.Tasks.Get(t.Context(), laid.task)
			require.NoError(t, err)
			if tt.wantStand != nil {
				tt.wantStand.ParentID = laid.parent.ID
				tt.wantStand.PullRequestID = xptr.Deref(laid.pullRequestID(t, fixture))
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

// TestRestackFoldsATaskWhosePullRequestMergedIntoItsParent covers a standing task merged into its
// parent's branch handing the tasks standing on it to that parent.
func TestRestackFoldsATaskWhosePullRequestMergedIntoItsParent(t *testing.T) {
	fixture := newRebaseFixture(t)
	ctx := t.Context()
	laid := fixture.seedStanding(t, agentstore.Agent{})
	middle, err := fixture.stores.Tasks.Get(ctx, laid.task)
	require.NoError(t, err)
	rider := fixture.stores.SeedTask(t, taskstore.CreateParams{
		WorkflowID: fixture.workflowID,
		Slug:       "rider",
		Stand:      &taskstore.Stand{ParentID: middle.ID, Tip: "middletip"},
	})
	merged := newStandPullRequest(middle.BranchName(), "MERGED")
	merged.BaseBranch = laid.parent.BranchName()
	_, err = fixture.stores.PullRequests.Upsert(ctx, merged)
	require.NoError(t, err)
	mover := newRestackMover(laid.parent.BranchName(), "newtip")

	require.NoError(t, fixture.newService(NewServiceParams{Git: mover}).Restack(ctx))

	folded, err := fixture.stores.Tasks.Get(ctx, middle.ID)
	require.NoError(t, err)
	assert.Nil(t, folded.Stand, "the merged task stands on nothing")

	moved, err := fixture.stores.Tasks.Get(ctx, rider.ID)
	require.NoError(t, err)
	assert.Equal(t, &taskstore.Stand{ParentID: laid.parent.ID, Tip: "middletip"}, moved.Stand)
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

// seedStanding records a writer on a task standing on a parent at oldtip, on a checkout that pushes to the test repository.
func (f *rebaseFixture) seedStanding(t *testing.T, writer agentstore.Agent) standing {
	t.Helper()

	parent := f.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: f.workflowID, Slug: "parent"})
	writer.Role = agentstore.RoleWriter
	seeded := f.seedPushable(t, writer, "")
	require.NoError(t, f.stores.Tasks.SetStand(t.Context(), *seeded.TaskID, &taskstore.Stand{ParentID: parent.ID, Tip: "oldtip"}))

	return standing{writer: seeded, task: *seeded.TaskID, parent: parent}
}

// pullRequestID is the parent's pull request the standing task follows, nil while it follows none.
func (s standing) pullRequestID(t *testing.T, fixture *rebaseFixture) *string {
	t.Helper()

	found, err := fixture.stores.PullRequests.FindStand(t.Context(), pullrequeststore.FindStandParams{
		RepositoryURL: pushRepository,
		HeadBranch:    s.parent.BranchName(),
	})
	if err != nil {
		return nil
	}

	return &found.ID
}

// seedStandPullRequest stores a pull request from the parent's branch onto main, in the state named.
func (f *rebaseFixture) seedStandPullRequest(t *testing.T, branch, state string) {
	t.Helper()

	_, err := f.stores.PullRequests.Upsert(t.Context(), newStandPullRequest(branch, state))
	require.NoError(t, err)
}

func newStandPullRequest(branch, state string) pullrequeststore.UpsertParams {
	opened := time.Now()
	params := pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        "PR_" + branch,
		URL:               pushRepository + "/pull/" + branch,
		RepositoryURL:     pushRepository,
		HeadRepositoryURL: pushRepository,
		Number:            1,
		HeadBranch:        branch,
		HeadSHA:           "newtip",
		BaseBranch:        "main",
		State:             state,
		OpenedAt:          opened,
		ChangedAt:         opened,
	}
	if state != "OPEN" {
		params.ClosedAt = &opened
	}
	if state == "MERGED" {
		params.MergedAt = &opened
	}

	return params
}

// newRestackMover is the git of a clean checkout on oldtip, with the parent's branch on origin at tip.
func newRestackMover(parentBranch, tip string) *gittest.Mover {
	return &gittest.Mover{
		State:   git.TreeState{Branch: "agent/a1"},
		Holds:   []string{"oldtip"},
		Tips:    map[string]string{parentBranch: tip},
		Commits: 2,
		SHA:     "pushedsha",
	}
}
