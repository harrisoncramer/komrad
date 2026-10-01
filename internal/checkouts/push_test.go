package checkouts

import (
	"testing"
	"time"

	"komrad/internal/git"
	"komrad/internal/gittest"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
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
		pusher  gittest.Pusher
		landed  bool
		wantErr error
	}

	tests := []testCase{
		{
			name:    "an agent whose tree something else is working in",
			agent:   agentstore.Agent{Cwd: "/locked"},
			pusher:  gittest.Pusher{State: git.TreeState{Branch: "agent/a1"}, Own: 1},
			wantErr: ErrBusy,
		},
		{
			name:    "a checkout part-way through a sequence",
			pusher:  gittest.Pusher{State: git.TreeState{Branch: "agent/a1", InProgress: true}, Own: 1},
			wantErr: ErrPushInProgress,
		},
		{
			name:    "a detached checkout",
			pusher:  gittest.Pusher{Own: 1},
			wantErr: ErrPushDetached,
		},
		{
			name:    "a checkout on the workflow's base branch",
			pusher:  gittest.Pusher{State: git.TreeState{Branch: "main"}, Own: 1},
			wantErr: ErrPushBase,
		},
		{
			name:    "a branch whose pull request has merged",
			pusher:  gittest.Pusher{State: git.TreeState{Branch: "agent/a1"}, Own: 1},
			landed:  true,
			wantErr: ErrPushFinished,
		},
		{
			name:    "a branch holding nothing the base does not, as a rebase onto landed work leaves it",
			pusher:  gittest.Pusher{State: git.TreeState{Branch: "agent/a1"}},
			wantErr: ErrPushNothing,
		},
		{
			name:  "a branch a human made, whose copy on origin it would overwrite",
			agent: agentstore.Agent{AdoptedBranch: "harry/spike"},
			pusher: gittest.Pusher{
				State:  git.TreeState{Branch: "harry/spike"},
				Own:    1,
				Remote: git.RemoteStanding{Pushed: true, Unpushed: 1, Unpulled: 2},
			},
			wantErr: ErrPushAdoptedForce,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seedPushable(t, tt.agent)
			if tt.landed {
				fixture.seedPullRequest(t, tt.pusher.State.Branch, xptr.Ptr(time.Now()))
			}
			locks := NewTreeLocks()
			release, taken := locks.TryLock("/locked")
			require.True(t, taken)
			t.Cleanup(release)

			_, err := fixture.newService(NewServiceParams{Pusher: &tt.pusher, Locks: locks}).Push(t.Context(), agent.ID)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, tt.pusher.Pushed)
			assert.Zero(t, fixture.checkoutSync.Wakes, "nothing moved, so nothing needs reading back")
		})
	}
}

// TestPushSendsTheBranch covers what a push asks of git, what it answers and who hears of it.
func TestPushSendsTheBranch(t *testing.T) {
	type testCase struct {
		name       string
		agent      agentstore.Agent
		pusher     gittest.Pusher
		wantPlan   PushPlan
		wantMailed bool
	}

	tests := []testCase{
		{
			name: "a rebased branch overwrites origin's copy and the agent is told the new commit",
			pusher: gittest.Pusher{
				State:   git.TreeState{Branch: "agent/a1"},
				Own:     3,
				Remote:  git.RemoteStanding{Pushed: true, Unpushed: 3, Unpulled: 2},
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
			name: "a branch origin never had is counted against the base",
			pusher: gittest.Pusher{
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
			name:  "a branch a human made goes up when origin's copy is behind it",
			agent: agentstore.Agent{AdoptedBranch: "harry/spike"},
			pusher: gittest.Pusher{
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
			agent := fixture.seedPushable(t, tt.agent)
			service := fixture.newService(NewServiceParams{Pusher: &tt.pusher})

			plan, err := service.PlanPush(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPlan, *plan)

			result, err := service.Push(t.Context(), agent.ID)
			require.NoError(t, err)

			assert.Equal(t, "abcdef1234567", result.SHA)
			assert.Equal(t, []git.PushParams{{
				Dir:    "/tree",
				Branch: tt.wantPlan.Branch,
				Force:  tt.wantPlan.Forced,
			}}, tt.pusher.Pushed)
			assert.Equal(t, 1, fixture.checkoutSync.Wakes)
			assert.Equal(t, 1, fixture.pullRequestSync.Wakes)

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
	agent := fixture.seedPushable(t, agentstore.Agent{})
	pusher := &gittest.Pusher{
		State:   git.TreeState{Branch: "agent/a1"},
		Own:     1,
		Remote:  git.RemoteStanding{Pushed: true, Unpushed: 1, Unpulled: 1},
		PushErr: git.ErrPushRejected,
	}

	_, err := fixture.newService(NewServiceParams{Pusher: pusher}).Push(t.Context(), agent.ID)

	require.ErrorIs(t, err, git.ErrPushRejected)
	assert.Equal(t, 1, fixture.checkoutSync.Wakes, "origin may have been read afresh, so the counts are read back")
	assert.Empty(t, fixture.readInbox(t, agent.ID), "nothing on origin moved")
}

// seedPushable records an agent whose checkout pushes to the test repository.
func (f *rebaseFixture) seedPushable(t *testing.T, agent agentstore.Agent) *agentstore.Agent {
	t.Helper()

	seeded := f.seed(t, agent)
	require.NoError(t, f.stores.Agents.SetCheckout(t.Context(), seeded.ID, agentstore.SetCheckoutParams{
		RepositoryURL: xptr.Ptr(pushRepository),
	}))

	return seeded
}

// seedPullRequest stores a pull request from branch in the test repository.
func (f *rebaseFixture) seedPullRequest(t *testing.T, branch string, mergedAt *time.Time) {
	t.Helper()

	_, err := f.stores.PullRequests.Upsert(t.Context(), pullrequeststore.UpsertParams{
		Host:          "github.com",
		Forge:         "github",
		ProviderID:    "PR_" + branch,
		URL:           pushRepository + "/pull/1",
		RepositoryURL: pushRepository,
		Number:        1,
		HeadBranch:    branch,
		BaseBranch:    "main",
		State:         "MERGED",
		OpenedAt:      time.Now(),
		ChangedAt:     time.Now(),
		MergedAt:      mergedAt,
	})
	require.NoError(t, err)
}
