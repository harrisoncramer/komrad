package pullrequests

import (
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	taskstore "komrad/internal/store/task"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stackedOn is the pull request on another's branch, at a place in a GitHub stack or in none.
func stackedOn(pullRequest forge.PullRequest, base string, place *forge.StackPlace) forge.PullRequest {
	pullRequest.BaseBranch = base
	pullRequest.Stack = place

	return pullRequest
}

// parentOf makes the task of one agent the parent of another's.
func (f *syncFixture) parentOf(t *testing.T, parentAgentID, childAgentID string) {
	t.Helper()
	ctx := t.Context()

	parent, err := f.agents.GetByID(ctx, parentAgentID)
	require.NoError(t, err)
	child, err := f.agents.GetByID(ctx, childAgentID)
	require.NoError(t, err)
	require.NoError(t, f.stores.Tasks.SetParent(ctx, *child.TaskID, &taskstore.Parent{TaskID: *parent.TaskID, ForkPoint: "abc"}))
}

// TestSyncBuildsGitHubStacks covers the sync stacking each run of a parent's and its children's
// pull requests on GitHub.
func TestSyncBuildsGitHubStacks(t *testing.T) {
	repository := git.RemoteURL{Raw: syncRepository, Scheme: "https", Host: "github.com", Path: "harrisoncramer/komrad"}
	autoMerged := pullRequestAt("PR_1", "task/one", syncNow.Add(-time.Hour))
	autoMerged.MergeReadiness.AutoMerge = forge.AutoMerge{On: true, Allowed: true}

	tests := []struct {
		name         string
		pullRequests []forge.PullRequest
		refuseStack  error
		wantStacked  []forge.StackParams
	}{
		{
			name: "a run GitHub refused is not asked again while it stands as refused",
			pullRequests: []forge.PullRequest{
				pullRequestAt("PR_1", "task/one", syncNow.Add(-time.Hour)),
				stackedOn(pullRequestAt("PR_2", "task/two", syncNow.Add(-time.Hour)), "task/one", nil),
			},
			refuseStack: forge.ErrNotStackable,
			wantStacked: []forge.StackParams{{Repo: repository, Numbers: []int{1, 2}}},
		},
		{
			name: "a pull request with auto-merge on starts no stack",
			pullRequests: []forge.PullRequest{
				autoMerged,
				stackedOn(pullRequestAt("PR_2", "task/two", syncNow.Add(-time.Hour)), "task/one", nil),
			},
		},
		{
			name: "a child whose pull request targets another branch is not stacked",
			pullRequests: []forge.PullRequest{
				pullRequestAt("PR_1", "task/one", syncNow.Add(-time.Hour)),
				pullRequestAt("PR_2", "task/two", syncNow.Add(-time.Hour)),
			},
		},
		{
			name: "a child on top of a GitHub stack is added to it",
			pullRequests: []forge.PullRequest{
				stackedOn(pullRequestAt("PR_1", "task/one", syncNow.Add(-time.Hour)), "main", &forge.StackPlace{Number: 7, Position: 1, Size: 2}),
				stackedOn(pullRequestAt("PR_2", "task/two", syncNow.Add(-time.Hour)), "task/one", &forge.StackPlace{Number: 7, Position: 2, Size: 2}),
				stackedOn(pullRequestAt("PR_3", "task/three", syncNow.Add(-time.Hour)), "task/two", nil),
			},
			wantStacked: []forge.StackParams{{Repo: repository, Stack: 7, Numbers: []int{3}}},
		},
		{
			name: "a parent's and its children's pull requests make a new GitHub stack",
			pullRequests: []forge.PullRequest{
				pullRequestAt("PR_1", "task/one", syncNow.Add(-time.Hour)),
				stackedOn(pullRequestAt("PR_2", "task/two", syncNow.Add(-time.Hour)), "task/one", nil),
				stackedOn(pullRequestAt("PR_3", "task/three", syncNow.Add(-time.Hour)), "task/two", nil),
			},
			wantStacked: []forge.StackParams{{Repo: repository, Numbers: []int{1, 2, 3}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			one := f.agentOn(t, "one", "task/one")
			two := f.agentOn(t, "two", "task/two")
			three := f.agentOn(t, "three", "task/three")
			f.parentOf(t, one, two)
			f.parentOf(t, two, three)
			f.forge.refuseStack = tt.refuseStack
			f.hold(tt.pullRequests...)

			f.sync(t)
			if tt.refuseStack != nil {
				f.syncer.Wake()
				f.pass(t, syncNow.Add(time.Minute))
			}

			if tt.wantStacked == nil {
				assert.Empty(t, f.forge.stacked)

				return
			}
			assert.Equal(t, tt.wantStacked, f.forge.stacked)
		})
	}
}

// TestSyncAwaitsGitHubRebasingAStack covers a stacked pull request's base branch changing, which
// leaves it and every open pull request over it waiting on GitHub to rebase them.
func TestSyncAwaitsGitHubRebasingAStack(t *testing.T) {
	f := newSyncFixture(t)
	one := f.agentOn(t, "one", "task/one")
	two := f.agentOn(t, "two", "task/two")
	three := f.agentOn(t, "three", "task/three")
	f.parentOf(t, one, two)
	f.parentOf(t, two, three)
	bottom := stackedOn(pullRequestAt("PR_1", "task/one", syncNow.Add(-time.Hour)), "main", &forge.StackPlace{Number: 7, Position: 1, Size: 3})
	middle := stackedOn(pullRequestAt("PR_2", "task/two", syncNow.Add(-time.Hour)), "task/one", &forge.StackPlace{Number: 7, Position: 2, Size: 3})
	top := stackedOn(pullRequestAt("PR_3", "task/three", syncNow.Add(-time.Hour)), "task/two", &forge.StackPlace{Number: 7, Position: 3, Size: 3})
	top.HeadSHA = "top"
	f.hold(bottom, middle, top)
	f.sync(t)
	assert.Nil(t, f.stored(t, "PR_2").RebaseAwait, "nothing waits before a base branch moves")

	bottom.State = "MERGED"
	bottom.UpdatedAt = syncNow
	middle.BaseBranch = "main"
	middle.HeadSHA = "rebased"
	middle.UpdatedAt = syncNow
	f.forge.pullRequests["harrisoncramer/komrad"] = []forge.PullRequest{bottom, middle, top}
	f.syncer.Wake()
	f.pass(t, syncNow.Add(time.Minute))

	assert.Nil(t, f.stored(t, "PR_1").RebaseAwait, "a merged pull request waits on nothing")
	middleAwait := f.stored(t, "PR_2").RebaseAwait
	require.NotNil(t, middleAwait)
	assert.Equal(t, scriptedHeadSHA, middleAwait.From, "the moved pull request waits from the head it had before the read")
	assert.True(t, middleAwait.Since.Equal(syncNow.Add(time.Minute)))
	topAwait := f.stored(t, "PR_3").RebaseAwait
	require.NotNil(t, topAwait)
	assert.Equal(t, "top", topAwait.From, "a pull request over it waits from its head")
}
