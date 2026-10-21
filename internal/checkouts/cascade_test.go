package checkouts

import (
	"encoding/json"
	"errors"
	"testing"

	"komrad/internal/forge"
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

// taskOnParent is a child and its parent, as the cascade tests seed them.
type taskOnParent struct {
	writer *agentstore.Agent
	task   string
	parent *taskstore.Task
}

// TestCascade covers one pass of the cascade over a child.
func TestCascade(t *testing.T) {
	tests := []struct {
		name string
		// writer is the task's writer as it is seeded.
		writer agentstore.Agent
		// seed writes what the case needs beyond the task and its parent.
		seed func(t *testing.T, fixture *rebaseFixture, seeded taskOnParent)
		// mover is the git of the task's checkout, told the parent's branch.
		mover func(parentBranch string) *gittest.Mover
		// wantParent is the parent read back after the pass, nil once the task is on the base branch.
		wantParent *taskstore.Parent
		wantCalls  []string
		// wantPush is whether komrad force-pushed the branch.
		wantPush       bool
		wantMail       string
		wantRetargeted []retargeted
	}{
		{
			name: "a parent whose newest commit is the fork point leaves the child alone",
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "oldtip")
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip"},
		},
		{
			name: "a parent not yet on origin leaves the child alone",
			mover: func(string) *gittest.Mover {
				return newChildGit("task/elsewhere", "newtip")
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip"},
		},
		{
			name:   "a writer part way through a turn is left to finish it",
			writer: agentstore.Agent{Pane: "%1", State: agentstore.StateThinking},
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip"},
		},
		{
			name:   "a writer part way through a turn on a branch holding neither commit is not orphaned",
			writer: agentstore.Agent{Pane: "%1", State: agentstore.StateThinking},
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = nil

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip"},
		},
		{
			name: "a worktree with changes not committed is left alone",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.State.TrackedChanges = true

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip"},
		},
		{
			name: "a branch holding neither its fork point nor the parent's newest commit is orphaned and the writer told",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = nil

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip", Blocked: taskstore.CascadeOrphaned},
			wantMail:   "Your branch no longer holds oldtip, the commit of parent your own commits sat on",
		},
		{
			name: "a branch that dropped its fork point but holds the base, with no re-target onto the base, is orphaned",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = []string{"basesha"}

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip", Blocked: taskstore.CascadeOrphaned},
			wantMail:   "Your branch no longer holds oldtip, the commit of parent your own commits sat on",
		},
		{
			name: "a re-target onto the base still stopped on conflicts is left alone",
			seed: seedStoppedRetarget,
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.State.InProgress = true
				mover.Holds = []string{"basesha"}

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip"},
		},
		{
			name: "a re-target onto the base the writer aborted keeps the parent",
			seed: seedStoppedRetarget,
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "oldtip")
				mover.Holds = []string{"oldtip", "basesha"}

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip"},
		},
		{
			name: "a rebase that conflicts is backed out and the writer told the rebase to run",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.ReplayErr = git.ErrRebaseConflicted
				mover.Prediction = git.Prediction{ConflictPaths: []string{"greet.go"}}

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip", Blocked: taskstore.CascadeConflicted},
			wantCalls:  []string{"park", "rebaseOnto", "abort rebase"},
			wantMail:   "conflicts in greet.go. Your branch is as it was. Run\ngit rebase --onto newtip oldtip",
		},
		{
			name: "a child whose rebase conflicted is not rebased again",
			seed: func(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
				require.NoError(t, fixture.stores.Tasks.SetCascadeBlocked(t.Context(), seeded.task, taskstore.CascadeConflicted))
			},
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip", Blocked: taskstore.CascadeConflicted},
		},
		{
			name: "a parent pull request closed without merging blocks the child and tells the writer",
			seed: func(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
				seedParentPullRequest(t, fixture, seeded, "CLOSED")
			},
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantParent: &taskstore.Parent{ForkPoint: "oldtip", Blocked: taskstore.CascadeParentClosed},
			wantMail:   "The pull request of parent, the task yours depends on, closed without merging",
		},
		{
			name: "a rebased branch origin refuses is left for the writer to push",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Remote = git.RemoteStanding{Pushed: true}
				mover.PushErr = errors.New("stale info")

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "newtip"},
			wantCalls:  []string{"park", "rebaseOnto"},
			wantPush:   true,
			wantMail:   "Komrad rebased your 2 commits onto it.",
		},
		{
			name: "a child whose rebase conflicted takes the parent's newest commit as its fork point once its branch holds it",
			seed: func(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
				require.NoError(t, fixture.stores.Tasks.SetCascadeBlocked(t.Context(), seeded.task, taskstore.CascadeConflicted))
			},
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = []string{"newtip"}

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "newtip"},
		},
		{
			name: "a branch already holding the parent's newest commit takes it as its fork point without a rebase",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Holds = append(mover.Holds, "newtip")

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "newtip"},
		},
		{
			name: "a re-target onto the base the writer finished clears the parent while the parent stays put",
			seed: seedStoppedRetarget,
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "oldtip")
				mover.Holds = []string{"basesha"}

				return mover
			},
		},
		{
			name: "a merged parent pull request has the child rebased onto the base branch",
			seed: func(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
				seedParentPullRequest(t, fixture, seeded, "MERGED")
			},
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantCalls: []string{"park", "rebaseOnto"},
			wantMail:  "parent merged into main. Komrad rebased your 2 commits onto main. Once origin holds your branch, komrad points your pull request at main.",
		},
		{
			name: "a merged parent pull request has the child's pushed pull request pointed at the base branch",
			seed: func(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
				seedParentPullRequest(t, fixture, seeded, "MERGED")
				seedChildPullRequest(t, fixture, seeded)
			},
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Remote = git.RemoteStanding{Pushed: true}

				return mover
			},
			wantCalls:      []string{"park", "rebaseOnto"},
			wantPush:       true,
			wantRetargeted: []retargeted{{url: pushRepository + "/pull/2", base: "main"}},
			wantMail:       "parent merged into main. Komrad rebased your 2 commits onto main and pushed.\n\n",
		},
		{
			name: "a merged parent pull request leaves the child's pull request alone while origin refuses the rebased branch",
			seed: func(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
				seedParentPullRequest(t, fixture, seeded, "MERGED")
				seedChildPullRequest(t, fixture, seeded)
			},
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Remote = git.RemoteStanding{Pushed: true, Unpushed: 2, Unpulled: 2}
				mover.PushErr = errors.New("stale info")

				return mover
			},
			wantCalls: []string{"park", "rebaseOnto"},
			wantPush:  true,
			wantMail:  "Once origin holds your branch, komrad points your pull request at main.",
		},
		{
			name: "a merged parent pull request still tells the writer when reading origin fails",
			seed: func(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
				seedParentPullRequest(t, fixture, seeded, "MERGED")
				seedChildPullRequest(t, fixture, seeded)
			},
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.RemoteErr = errors.New("no origin")

				return mover
			},
			wantCalls: []string{"park", "rebaseOnto"},
			wantMail:  "Once origin holds your branch, komrad points your pull request at main.",
		},
		{
			name: "a rebased branch origin already holds is force-pushed",
			mover: func(parentBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Remote = git.RemoteStanding{Pushed: true}

				return mover
			},
			wantParent: &taskstore.Parent{ForkPoint: "newtip"},
			wantCalls:  []string{"park", "rebaseOnto"},
			wantPush:   true,
			wantMail:   "onto it and pushed.",
		},
		{
			name: "a parent with a newer commit has the child's own commits rebased onto it",
			mover: func(parentBranch string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantParent: &taskstore.Parent{ForkPoint: "newtip"},
			wantCalls:  []string{"park", "rebaseOnto"},
			wantMail:   "parent moved to newtip. Komrad rebased your 2 commits onto it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			seeded := fixture.seedTaskOnParent(t, tt.writer)
			if tt.seed != nil {
				tt.seed(t, fixture, seeded)
			}
			mover := tt.mover(seeded.parent.GetBranch())

			require.NoError(t, fixture.newService(NewServiceParams{Git: mover}).Cascade(t.Context()))

			got, err := fixture.stores.Tasks.Get(t.Context(), seeded.task)
			require.NoError(t, err)
			if tt.wantParent != nil {
				tt.wantParent.TaskID = seeded.parent.ID
				tt.wantParent.PullRequestID = xptr.Deref(findPullRequestID(t, fixture, seeded))
			}
			assert.Equal(t, tt.wantParent, got.Parent)
			assert.Subset(t, mover.Calls, tt.wantCalls)
			if tt.wantCalls == nil {
				assert.NotContains(t, mover.Calls, "rebaseOnto", "nothing is rebased")
			}
			assert.Equal(t, tt.wantPush, len(mover.Pushed) == 1 && mover.Pushed[0].Force)
			assert.Equal(t, tt.wantRetargeted, fixture.forges.retargeted)

			inbox := fixture.readInbox(t, seeded.writer.ID)
			if tt.wantMail == "" {
				assert.Empty(t, inbox)

				return
			}
			require.Len(t, inbox, 1)
			assert.Contains(t, inbox[0], tt.wantMail)
		})
	}
}

// TestCascadeMerged covers moving the children of a pull request komrad merged before the pull request
// sync reads it merged.
func TestCascadeMerged(t *testing.T) {
	tests := []struct {
		name       string
		merged     string
		wantParent bool
		wantCalls  []string
	}{
		{
			name:      "the parent's pull request has the child rebased onto the base branch",
			merged:    pushRepository + "/pull/1",
			wantCalls: []string{"park", "rebaseOnto"},
		},
		{
			name:       "another pull request leaves the child alone",
			merged:     pushRepository + "/pull/9",
			wantParent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			seeded := fixture.seedTaskOnParent(t, agentstore.Agent{})
			seedParentPullRequest(t, fixture, seeded, "OPEN")
			mover := newChildGit(seeded.parent.GetBranch(), "newtip")

			require.NoError(t, fixture.newService(NewServiceParams{Git: mover}).CascadeMerged(t.Context(), tt.merged))

			got, err := fixture.stores.Tasks.Get(t.Context(), seeded.task)
			require.NoError(t, err)
			assert.Equal(t, tt.wantParent, got.Parent != nil)
			assert.Subset(t, mover.Calls, tt.wantCalls)
			if tt.wantCalls == nil {
				assert.NotContains(t, mover.Calls, "rebaseOnto", "nothing is rebased")
			}
		})
	}
}

// TestCascadeRetargetsTasksThatLeftTheirParent covers a task that left its parent while its pull
// request still targets the parent's branch.
func TestCascadeRetargetsTasksThatLeftTheirParent(t *testing.T) {
	onto := retargeted{url: pushRepository + "/pull/2", base: "main"}

	tests := []struct {
		name           string
		parentState    string
		remote         git.RemoteStanding
		forgeErr       error
		passes         int
		wantRetargeted []retargeted
		wantMail       string
	}{
		{
			name:           "a branch origin holds has its pull request pointed at the base branch",
			parentState:    "MERGED",
			remote:         git.RemoteStanding{Pushed: true},
			passes:         1,
			wantRetargeted: []retargeted{onto},
		},
		{
			name:           "a branch that left a parent whose pull request is open has its pull request pointed at the base branch",
			parentState:    "OPEN",
			remote:         git.RemoteStanding{Pushed: true},
			passes:         1,
			wantRetargeted: []retargeted{onto},
		},
		{
			name:        "a branch with commits origin lacks is left until they are pushed",
			parentState: "MERGED",
			remote:      git.RemoteStanding{Pushed: true, Unpushed: 2},
			passes:      1,
		},
		{
			name:           "a forge that refuses is asked once while the pull request stands still",
			parentState:    "MERGED",
			remote:         git.RemoteStanding{Pushed: true},
			forgeErr:       forge.ErrNotRetargetable,
			passes:         2,
			wantRetargeted: []retargeted{onto},
			wantMail:       "Point it at main yourself.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			fixture.forges.err = tt.forgeErr
			seeded := fixture.seedTaskOnParent(t, agentstore.Agent{})
			claimed, err := fixture.stores.Tasks.ClaimCheckout(t.Context(), taskstore.ClaimCheckoutParams{
				ID:            seeded.parent.ID,
				RepositoryURL: pushRepository,
			})
			require.NoError(t, err)
			require.True(t, claimed)
			require.NoError(t, fixture.stores.Tasks.SetParent(t.Context(), seeded.task, nil))
			seedParentPullRequest(t, fixture, seeded, tt.parentState)
			seedChildPullRequest(t, fixture, seeded)
			service := fixture.newService(NewServiceParams{Git: &gittest.Mover{Remote: tt.remote}})

			for range tt.passes {
				require.NoError(t, service.Cascade(t.Context()))
			}

			assert.Equal(t, tt.wantRetargeted, fixture.forges.retargeted)

			inbox := fixture.readInbox(t, seeded.writer.ID)
			if tt.wantMail == "" {
				assert.Empty(t, inbox)

				return
			}
			require.Len(t, inbox, 1)
			assert.Contains(t, inbox[0], tt.wantMail)
		})
	}
}

// TestCascadeLeavesAChildThatLeftItsParentSinceThePassListedIt covers a pass working from a task read
// before another pass moved the child onto the base branch.
func TestCascadeLeavesAChildThatLeftItsParentSinceThePassListedIt(t *testing.T) {
	fixture := newRebaseFixture(t)
	seeded := fixture.seedTaskOnParent(t, agentstore.Agent{})
	listed, err := fixture.stores.Tasks.Get(t.Context(), seeded.task)
	require.NoError(t, err)
	require.NoError(t, fixture.stores.Tasks.SetParent(t.Context(), seeded.task, nil))
	mover := newChildGit(seeded.parent.GetBranch(), "newtip")

	require.NoError(t, fixture.newService(NewServiceParams{Git: mover}).cascadeTask(t.Context(), *listed, ""))

	got, err := fixture.stores.Tasks.Get(t.Context(), seeded.task)
	require.NoError(t, err)
	assert.Nil(t, got.Parent)
	assert.NotContains(t, mover.Calls, "rebaseOnto")
	assert.Empty(t, fixture.readInbox(t, seeded.writer.ID))
}

// TestCascadeOrphansABranchThatAbortedARetarget covers a writer aborting a stopped re-target onto the
// base branch, and the branch dropping its fork point on a later pass.
func TestCascadeOrphansABranchThatAbortedARetarget(t *testing.T) {
	fixture := newRebaseFixture(t)
	seeded := fixture.seedTaskOnParent(t, agentstore.Agent{})
	seedStoppedRetarget(t, fixture, seeded)
	mover := newChildGit(seeded.parent.GetBranch(), "oldtip")
	mover.Holds = []string{"oldtip", "basesha"}
	service := fixture.newService(NewServiceParams{Git: mover})

	require.NoError(t, service.Cascade(t.Context()))

	mover.Tips[seeded.parent.GetBranch()] = "newtip"
	mover.Holds = []string{"basesha"}

	require.NoError(t, service.Cascade(t.Context()))

	got, err := fixture.stores.Tasks.Get(t.Context(), seeded.task)
	require.NoError(t, err)
	require.NotNil(t, got.Parent)
	assert.Equal(t, taskstore.CascadeOrphaned, got.Parent.Blocked)
}

// TestCascadeSettlesAChildWhosePullRequestMergedIntoItsParent covers a task merged into its
// parent's branch handing its children to that parent.
func TestCascadeSettlesAChildWhosePullRequestMergedIntoItsParent(t *testing.T) {
	fixture := newRebaseFixture(t)
	ctx := t.Context()
	seeded := fixture.seedTaskOnParent(t, agentstore.Agent{})
	merged, err := fixture.stores.Tasks.Get(ctx, seeded.task)
	require.NoError(t, err)
	child := fixture.stores.SeedTask(t, taskstore.CreateParams{
		WorkflowID: fixture.workflowID,
		Slug:       "child",
		Parent:     &taskstore.Parent{TaskID: merged.ID, ForkPoint: "mergedtip"},
	})
	fixture.stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
		RepositoryURL: pushRepository,
		Number:        1,
		HeadBranch:    merged.GetBranch(),
		HeadSHA:       "mergedtip",
		BaseBranch:    seeded.parent.GetBranch(),
		State:         "MERGED",
	})
	mover := newChildGit(seeded.parent.GetBranch(), "newtip")

	require.NoError(t, fixture.newService(NewServiceParams{Git: mover}).Cascade(ctx))

	settled, err := fixture.stores.Tasks.Get(ctx, merged.ID)
	require.NoError(t, err)
	assert.Nil(t, settled.Parent, "the merged task has no parent")

	moved, err := fixture.stores.Tasks.Get(ctx, child.ID)
	require.NoError(t, err)
	assert.Equal(t, &taskstore.Parent{TaskID: seeded.parent.ID, ForkPoint: "mergedtip"}, moved.Parent)
	assert.NotContains(t, mover.Calls, "rebaseOnto")
}

// TestPickForkPoint covers picking the fork point of a child.
func TestPickForkPoint(t *testing.T) {
	tests := []struct {
		name          string
		isChild       bool
		holds         []string
		wantErr       error
		wantForkPoint string
	}{
		{
			name:    "a task without a parent is refused",
			holds:   []string{"placed"},
			wantErr: errNoParent,
		},
		{
			name:          "a commit the branch does not hold is refused",
			isChild:       true,
			wantErr:       errForkPointNotHeld,
			wantForkPoint: "oldtip",
		},
		{
			name:          "a commit the branch holds becomes the fork point",
			isChild:       true,
			holds:         []string{"placed"},
			wantForkPoint: "placed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{})
			if tt.isChild {
				parent := fixture.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: fixture.workflowID, Slug: "parent"})
				require.NoError(t, fixture.stores.Tasks.SetParent(t.Context(), *agent.TaskID, &taskstore.Parent{TaskID: parent.ID, ForkPoint: "oldtip"}))
			}
			mover := &gittest.Mover{Holds: tt.holds}

			sha, err := fixture.newService(NewServiceParams{Git: mover}).PickForkPoint(t.Context(), PickForkPointParams{Task: *agent.TaskID, Commit: "placed"})

			got, readErr := fixture.stores.Tasks.Get(t.Context(), *agent.TaskID)
			require.NoError(t, readErr)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "placed", sha)
				assert.Equal(t, 1, fixture.checkoutSync.Wakes)
			}
			if tt.wantForkPoint != "" {
				assert.Equal(t, tt.wantForkPoint, got.Parent.ForkPoint)
			}
		})
	}
}

// seedTaskOnParent records a writer on a child with its fork point at oldtip, on a checkout that pushes to the test repository.
func (f *rebaseFixture) seedTaskOnParent(t *testing.T, writer agentstore.Agent) taskOnParent {
	t.Helper()

	parent := f.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: f.workflowID, Slug: "parent"})
	writer.Role = agentstore.RoleWriter
	seeded := f.seedPushable(t, writer, "")
	require.NoError(t, f.stores.Tasks.SetParent(t.Context(), *seeded.TaskID, &taskstore.Parent{TaskID: parent.ID, ForkPoint: "oldtip"}))

	return taskOnParent{writer: seeded, task: *seeded.TaskID, parent: parent}
}

// findPullRequestID is the child's parent pull request, nil while it has none.
func findPullRequestID(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) *string {
	t.Helper()

	found, err := fixture.stores.PullRequests.GetParentPullRequest(t.Context(), pullrequeststore.GetParentPullRequestParams{
		RepositoryURL: pushRepository,
		HeadBranch:    seeded.parent.GetBranch(),
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	require.NoError(t, err)

	return &found.ID
}

// seedParentPullRequest stores a pull request from the parent's branch onto main at newtip, in the state named.
func seedParentPullRequest(t *testing.T, fixture *rebaseFixture, seeded taskOnParent, state string) {
	t.Helper()

	fixture.stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
		RepositoryURL: pushRepository,
		Number:        1,
		HeadBranch:    seeded.parent.GetBranch(),
		HeadSHA:       "newtip",
		BaseBranch:    "main",
		State:         state,
	})
}

// seedChildPullRequest stores an open pull request from the child's branch onto the parent's.
func seedChildPullRequest(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
	t.Helper()

	child, err := fixture.stores.Tasks.Get(t.Context(), seeded.task)
	require.NoError(t, err)
	fixture.stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
		RepositoryURL: pushRepository,
		Number:        2,
		HeadBranch:    child.GetBranch(),
		HeadSHA:       "pushedsha",
		BaseBranch:    seeded.parent.GetBranch(),
	})
}

// seedStoppedRetarget records a re-target of the child onto the base branch at basesha that stopped on conflicts.
func seedStoppedRetarget(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
	t.Helper()

	raw, err := json.Marshal(rebaseOutcome{Result: &RebaseResult{
		RebasePlan: RebasePlan{BaseRef: "origin/main", BaseSHA: "basesha"},
		Conflicted: true,
		OntoBase:   true,
	}})
	require.NoError(t, err)
	require.NoError(t, fixture.stores.Tasks.SetLastRebase(t.Context(), seeded.task, raw))
}

// newChildGit is the git of a clean checkout on oldtip, with the parent's branch on origin at newest.
func newChildGit(parentBranch, newest string) *gittest.Mover {
	return &gittest.Mover{
		State:   git.TreeState{Branch: "agent/a1"},
		Holds:   []string{"oldtip"},
		Tips:    map[string]string{parentBranch: newest},
		Commits: 2,
		SHA:     "pushedsha",
	}
}
