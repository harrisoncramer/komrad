package checkouts

import (
	"context"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/gittest"
	agentstore "komrad/internal/store/agent"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/internal/storetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCascadeFollowsAGitHubStack covers a pass of the cascade over a child whose pull request sits on
// its parent's in a GitHub stack, after the parent's merged.
func TestCascadeFollowsAGitHubStack(t *testing.T) {
	tests := []struct {
		name string
		// ownBase is the base branch of the child's pull request, the parent's branch when empty.
		ownBase string
		// ownHead is the head of the child's pull request.
		ownHead string
		// awaitFrom is the head komrad waits on GitHub to rebase from, waiting on nothing when empty.
		awaitFrom string
		// clock is how long after the parent merged the pass runs.
		clock time.Duration
		// mover is the git of the task's checkout, told the parent's and the child's branches.
		mover func(parentBranch, ownBranch string) *gittest.Mover
		// wantOnParent says the task still has its parent after the pass.
		wantOnParent bool
		wantAwait    bool
		wantCalls    []string
		wantCutoff   string
		wantMail     string
	}{
		{
			name:    "a child whose parent merged a moment ago waits on GitHub to change its base branch",
			ownHead: "oldhead",
			mover: func(parentBranch, _ string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantOnParent: true,
		},
		{
			name:      "a child GitHub has not rebased yet waits on it",
			ownBase:   "main",
			ownHead:   "oldhead",
			awaitFrom: "oldhead",
			mover: func(parentBranch, _ string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantOnParent: true,
			wantAwait:    true,
		},
		{
			name:      "a child whose branch dropped what it pushed stops waiting on GitHub",
			ownBase:   "main",
			ownHead:   "ghhead",
			awaitFrom: "oldhead",
			mover: func(parentBranch, ownBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Tips[ownBranch] = "ghhead"

				return mover
			},
			wantOnParent: true,
		},
		{
			name:      "a child whose commits conflict with GitHub's rebase is backed out and the writer told",
			ownBase:   "main",
			ownHead:   "ghhead",
			awaitFrom: "oldhead",
			mover: func(parentBranch, ownBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Tips[ownBranch] = "ghhead"
				mover.Holds = append(mover.Holds, "oldhead")
				mover.ReplayErr = git.ErrRebaseConflicted

				return mover
			},
			wantOnParent: true,
			wantCalls:    []string{"park", "rebaseOnto", "abort rebase"},
			wantCutoff:   "oldhead",
			wantMail:     "git rebase --onto ghhead oldhead",
		},
		{
			name:      "a child GitHub did not rebase in time is rebased by the cascade",
			ownBase:   "main",
			ownHead:   "oldhead",
			awaitFrom: "oldhead",
			clock:     2 * time.Minute,
			mover: func(parentBranch, _ string) *gittest.Mover {
				return newChildGit(parentBranch, "newtip")
			},
			wantCalls: []string{"park", "rebaseOnto"},
			wantMail:  "parent merged into main. Komrad rebased your 2 commits onto main.\n\n",
		},
		{
			name:      "a child GitHub rebased has its worktree moved onto GitHub's branch",
			ownBase:   "main",
			ownHead:   "ghhead",
			awaitFrom: "oldhead",
			mover: func(parentBranch, ownBranch string) *gittest.Mover {
				mover := newChildGit(parentBranch, "newtip")
				mover.Tips[ownBranch] = "ghhead"
				mover.Holds = append(mover.Holds, "oldhead")

				return mover
			},
			wantCalls:  []string{"park", "rebaseOnto"},
			wantCutoff: "oldhead",
			wantMail:   "parent merged into main. GitHub rebased your 2 commits onto main and changed your pull request's base branch to main. Komrad moved your worktree to match.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			ctx := t.Context()
			seeded := fixture.seedTaskOnParent(t, agentstore.Agent{})
			child, err := fixture.stores.Tasks.Get(ctx, seeded.task)
			require.NoError(t, err)
			now := time.Now()
			fixture.stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
				RepositoryURL: pushRepository,
				Number:        1,
				HeadBranch:    seeded.parent.GetBranch(),
				HeadSHA:       "newtip",
				BaseBranch:    "main",
				State:         "MERGED",
				OpenedAt:      now,
			})
			base := tt.ownBase
			if base == "" {
				base = seeded.parent.GetBranch()
			}
			own := fixture.stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
				RepositoryURL: pushRepository,
				Number:        2,
				HeadBranch:    child.GetBranch(),
				HeadSHA:       tt.ownHead,
				BaseBranch:    base,
				OpenedAt:      now,
				Stack:         &forge.StackPlace{Number: 7, Position: 2, Size: 2},
			})
			if tt.awaitFrom != "" {
				require.NoError(t, fixture.stores.PullRequests.RecordRebaseAwait(ctx, pullrequeststore.RecordRebaseAwaitParams{
					ID:      own.ID,
					Since:   now,
					HeadSHA: tt.awaitFrom,
				}))
			}
			mover := tt.mover(seeded.parent.GetBranch(), child.GetBranch())
			service := fixture.newService(NewServiceParams{
				Git:   mover,
				Clock: func() time.Time { return now.Add(tt.clock) },
			})

			require.NoError(t, service.Cascade(ctx))

			got, err := fixture.stores.Tasks.Get(ctx, seeded.task)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOnParent, got.Parent != nil)
			stored, err := fixture.stores.PullRequests.GetByID(ctx, own.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAwait, stored.RebaseAwait != nil)
			if tt.wantCalls == nil {
				assert.NotContains(t, mover.Calls, "rebaseOnto", "nothing is rebased")
			}
			assert.Subset(t, mover.Calls, tt.wantCalls)
			if tt.wantCutoff != "" {
				assert.Equal(t, tt.wantCutoff, mover.Cutoff)
			}

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

// retargeting records each pull request a re-target pointed at another base branch.
type retargeting struct {
	retargeted []string
}

func (r *retargeting) RetargetPullRequest(_ context.Context, address, base string) error {
	r.retargeted = append(r.retargeted, address+" onto "+base)

	return nil
}

// seedStackedPullRequest stores the child's pull request onto its parent's branch, on top of the parent's in a GitHub stack.
func seedStackedPullRequest(t *testing.T, fixture *rebaseFixture, seeded taskOnParent) {
	t.Helper()

	child, err := fixture.stores.Tasks.Get(t.Context(), seeded.task)
	require.NoError(t, err)
	fixture.stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
		RepositoryURL: pushRepository,
		Number:        2,
		HeadBranch:    child.GetBranch(),
		HeadSHA:       "oldhead",
		BaseBranch:    seeded.parent.GetBranch(),
		Stack:         &forge.StackPlace{Number: 7, Position: 2, Size: 2},
	})
}
