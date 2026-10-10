package checkouts

import (
	"slices"
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

const pushRepository = "https://github.com/acme/api"

// TestPushRefusals covers every state of the row and the checkout a push declines.
func TestPushRefusals(t *testing.T) {
	type testCase struct {
		name    string
		agent   agentstore.Agent
		adopted string
		pusher  gittest.Mover
		landed  bool
		wantErr error
	}

	tests := []testCase{
		{
			name:    "an agent whose tree something else is working in",
			agent:   agentstore.Agent{Cwd: "/locked"},
			pusher:  gittest.Mover{State: git.TreeState{Branch: "agent/a1"}, Own: 1},
			wantErr: errBusy,
		},
		{
			name:    "a checkout part-way through a sequence",
			pusher:  gittest.Mover{State: git.TreeState{Branch: "agent/a1", InProgress: true}, Own: 1},
			wantErr: errPushInProgress,
		},
		{
			name:    "a detached checkout",
			pusher:  gittest.Mover{Own: 1},
			wantErr: errPushDetached,
		},
		{
			name:    "a checkout on the workflow's base branch",
			pusher:  gittest.Mover{State: git.TreeState{Branch: "main"}, Own: 1},
			wantErr: errPushBase,
		},
		{
			name:    "a branch whose pull request has merged, still on the commit it merged from",
			pusher:  gittest.Mover{State: git.TreeState{Branch: "agent/a1", HeadSHA: "merged"}, Own: 1},
			landed:  true,
			wantErr: errPushFinished,
		},
		{
			name: "a branch whose pull request has merged, with new work on top of the merged commits",
			pusher: gittest.Mover{
				State: git.TreeState{Branch: "agent/a1", HeadSHA: "followup"},
				Own:   1,
				Holds: []string{"merged"},
			},
			landed:  true,
			wantErr: errPushFinished,
		},
		{
			name:    "a branch whose pull request has merged, read from a checkout with no commit",
			pusher:  gittest.Mover{State: git.TreeState{Branch: "agent/a1"}, Own: 1},
			landed:  true,
			wantErr: errPushFinished,
		},
		{
			name:    "a branch holding nothing the base does not, as a rebase onto landed work leaves it",
			pusher:  gittest.Mover{State: git.TreeState{Branch: "agent/a1"}},
			wantErr: errPushNothing,
		},
		{
			name: "a branch origin already holds whole",
			pusher: gittest.Mover{
				State:  git.TreeState{Branch: "agent/a1"},
				Own:    1,
				Remote: git.RemoteStanding{Pushed: true},
			},
			wantErr: errPushInSync,
		},
		{
			name: "a branch behind origin's copy, which only a pull would bring level",
			pusher: gittest.Mover{
				State:  git.TreeState{Branch: "agent/a1"},
				Own:    1,
				Remote: git.RemoteStanding{Pushed: true, Unpulled: 1},
			},
			wantErr: git.ErrPushRejected,
		},
		{
			name:    "a branch a human made, whose copy on origin it would overwrite",
			adopted: "harry/spike",
			pusher: gittest.Mover{
				State: git.TreeState{Branch: "harry/spike"},
				Own:   1,
				Remote: git.RemoteStanding{
					Pushed:   true,
					Unpushed: 1,
					Unpulled: 2,
				},
			},
			wantErr: errPushAdoptedForce,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seedPushable(t, tt.agent, tt.adopted)
			if tt.landed {
				fixture.seedMerged(t, tt.pusher.State.Branch)
			}
			locks := newTreeLocks()
			release, taken := locks.tryLock("/locked")
			require.True(t, taken)
			t.Cleanup(release)

			_, err := fixture.newService(NewServiceParams{Git: &tt.pusher, Locks: locks}).Push(t.Context(), agent.ID)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, tt.pusher.Pushed)
			assert.Zero(t, fixture.checkoutSync.Wakes, "nothing moved, so nothing needs reading back")
		})
	}
}

// TestPlanPushInSync covers a branch origin already holds whole planning as nothing to push.
func TestPlanPushInSync(t *testing.T) {
	fixture := newRebaseFixture(t)
	agent := fixture.seedPushable(t, agentstore.Agent{}, "")
	pusher := gittest.Mover{
		State:  git.TreeState{Branch: "agent/a1"},
		Own:    1,
		Remote: git.RemoteStanding{Pushed: true},
	}

	plan, err := fixture.newService(NewServiceParams{Git: &pusher}).PlanPush(t.Context(), agent.ID)

	require.NoError(t, err)
	assert.Equal(t, PushPlan{Branch: "agent/a1", InSync: true}, *plan)
}

// TestPushSendsTheBranch covers what a push asks of git, what it answers and who hears of it.
func TestPushSendsTheBranch(t *testing.T) {
	type testCase struct {
		name       string
		adopted    string
		pusher     gittest.Mover
		landed     bool
		wantPlan   PushPlan
		wantMailed bool
	}

	tests := []testCase{
		{
			name: "a rebased branch overwrites origin's copy and the agent is told the new commit",
			pusher: gittest.Mover{
				State: git.TreeState{Branch: "agent/a1"},
				Own:   3,
				Remote: git.RemoteStanding{
					Pushed:   true,
					Unpushed: 3,
					Unpulled: 2,
				},
				Changes: git.Changes{Modified: []string{"a.go"}, Untracked: []string{"b.go"}},
				SHA:     "abcdef1234567",
			},
			wantPlan: PushPlan{
				Branch:      "agent/a1",
				Commits:     3,
				Forced:      true,
				Uncommitted: 2,
			},
			wantMailed: true,
		},
		{
			name: "a branch reset onto the base after its pull request merged overwrites origin's copy",
			pusher: gittest.Mover{
				State: git.TreeState{Branch: "agent/a1", HeadSHA: "followup"},
				Own:   1,
				Remote: git.RemoteStanding{
					Pushed:   true,
					Unpushed: 1,
					Unpulled: 4,
				},
				SHA: "abcdef1234567",
			},
			landed: true,
			wantPlan: PushPlan{
				Branch:  "agent/a1",
				Commits: 1,
				Forced:  true,
			},
			wantMailed: true,
		},
		{
			name: "a branch origin never had is counted against the base",
			pusher: gittest.Mover{
				State: git.TreeState{Branch: "agent/a1"},
				Own:   4,
				SHA:   "abcdef1234567",
			},
			wantPlan: PushPlan{
				Branch:  "agent/a1",
				Commits: 4,
			},
		},
		{
			name:    "a branch a human made goes up when origin's copy is behind it",
			adopted: "harry/spike",
			pusher: gittest.Mover{
				State:  git.TreeState{Branch: "harry/spike"},
				Own:    2,
				Remote: git.RemoteStanding{Pushed: true, Unpushed: 1},
				SHA:    "abcdef1234567",
			},
			wantPlan: PushPlan{
				Branch:  "harry/spike",
				Commits: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seedPushable(t, agentstore.Agent{}, tt.adopted)
			if tt.landed {
				fixture.seedMerged(t, tt.pusher.State.Branch)
			}
			service := fixture.newService(NewServiceParams{Git: &tt.pusher})

			plan, err := service.PlanPush(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPlan, *plan)
			assert.NotContains(t, tt.pusher.Calls, "refresh", "a preview leaves origin's copy alone")

			result, err := service.Push(t.Context(), agent.ID)
			require.NoError(t, err)

			assert.Equal(t, "abcdef1234567", result.SHA)
			assert.Equal(t, tt.landed, slices.Contains(tt.pusher.Calls, "refresh"), "a push reads origin's copy afresh only once its pull request has finished")
			assert.Equal(t, []git.PushParams{{
				Dir:    "/tree",
				Branch: tt.wantPlan.Branch,
				Force:  tt.wantPlan.Forced,
			}}, tt.pusher.Pushed)
			assert.Equal(t, 1, fixture.checkoutSync.Wakes)
			assert.Equal(t, []string{"github.com"}, fixture.pullRequestSync.Heated)

			inbox := fixture.readInbox(t, agent.ID)
			if !tt.wantMailed {
				assert.Empty(t, inbox, "a push that overwrote nothing changes nothing the agent knows")

				return
			}
			require.Len(t, inbox, 1)
			assert.Contains(t, inbox[0], "abcdef1")
		})
	}
}

// TestPushReportsWhatOriginSaid covers a refused push reaching the caller and still waking the syncs.
func TestPushReportsWhatOriginSaid(t *testing.T) {
	fixture := newRebaseFixture(t)
	agent := fixture.seedPushable(t, agentstore.Agent{}, "")
	pusher := &gittest.Mover{
		State: git.TreeState{Branch: "agent/a1"},
		Own:   1,
		Remote: git.RemoteStanding{
			Pushed:   true,
			Unpushed: 1,
			Unpulled: 1,
		},
		PushErr: git.ErrPushRejected,
	}

	_, err := fixture.newService(NewServiceParams{Git: pusher}).Push(t.Context(), agent.ID)

	require.ErrorIs(t, err, git.ErrPushRejected)
	assert.Equal(t, 1, fixture.checkoutSync.Wakes, "origin may have been read afresh, so the counts are read back")
	assert.Empty(t, fixture.readInbox(t, agent.ID), "nothing on origin moved")
}

// seedPushable records an agent whose task's checkout, on the branch adopted if one is named, pushes to the test repository.
func (f *rebaseFixture) seedPushable(t *testing.T, agent agentstore.Agent, adopted string) *agentstore.Agent {
	t.Helper()

	seeded := f.seed(t, agent)
	claimed, err := f.stores.Tasks.ClaimCheckout(t.Context(), taskstore.ClaimCheckoutParams{
		ID:            *seeded.TaskID,
		AdoptedBranch: adopted,
		RepositoryURL: pushRepository,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	return seeded
}

// seedMerged stores a pull request from branch in the test repository that merged from the commit named merged.
func (f *rebaseFixture) seedMerged(t *testing.T, branch string) {
	t.Helper()

	_, err := f.stores.PullRequests.Upsert(t.Context(), pullrequeststore.UpsertParams{
		Host:          "github.com",
		Forge:         "github",
		ProviderID:    "PR_" + branch,
		URL:           pushRepository + "/pull/1",
		RepositoryURL: pushRepository,
		Number:        1,
		HeadBranch:    branch,
		HeadSHA:       "merged",
		BaseBranch:    "main",
		State:         "MERGED",
		OpenedAt:      time.Now(),
		ChangedAt:     time.Now(),
		MergedAt:      xptr.Ptr(time.Now()),
	})
	require.NoError(t, err)
}
