package pullrequest

import (
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/git"
	pullrequeststore "weave/internal/store/pullrequest"
	"weave/internal/storetest"
	"weave/lib/xptr"
	"weave/lib/xslice"
	paginationv1 "weave/proto/gen/pagination/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1weave"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListPullRequests covers the stored listing: the repository named as a remote or as its
// canonical address, the branch narrowing it, the unfinished ones first and each group newest
// opened first, the state decided per row, and a page token walking the rest.
func TestListPullRequests(t *testing.T) {
	t.Parallel()

	st := storetest.Open(t)
	ctx := t.Context()
	service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewHandler(NewHandlerParams{PullRequests: st.PullRequests})}

	opened := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	mergedAt := opened.Add(3 * time.Hour)
	elsewhere := storedPullRequest("PR_4", 4, opened.Add(3*time.Hour), nil)
	elsewhere.HeadBranch = "agent/y"
	for _, params := range []pullrequeststore.UpsertParams{
		storedPullRequest("PR_1", 1, opened, nil),
		storedPullRequest("PR_2", 2, opened.Add(time.Hour), nil),
		storedPullRequest("PR_3", 3, opened.Add(2*time.Hour), &mergedAt),
		elsewhere,
	} {
		_, err := st.PullRequests.Upsert(ctx, params)
		require.NoError(t, err)
	}
	approved, err := st.PullRequests.GetByIdentity(ctx, "github.com", "PR_2")
	require.NoError(t, err)
	require.NoError(t, st.PullRequests.SetStanding(ctx, approved.ID, forge.Standing{Decision: forge.ReviewDecisionApproved, Mergeability: forge.MergeabilityClean}, time.Now()))

	numbers := func(pullRequests []*pullrequestv1.PullRequest) []int32 {
		return xslice.Map(pullRequests, func(pullRequest *pullrequestv1.PullRequest) int32 { return pullRequest.GetNumber() })
	}

	onBranch, err := service.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RepositoryUrl: "git@github.com:harrisoncramer/weave.git",
		HeadBranch:    "agent/x",
	}))
	require.NoError(t, err)
	assert.Equal(t, []int32{2, 1, 3}, numbers(onBranch.Msg.GetPullRequests()), "the unfinished first, newest opened first, then the merged")
	assert.Nil(t, onBranch.Msg.NextPageToken)
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED, onBranch.Msg.GetPullRequests()[0].GetState())
	assert.Equal(t, pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED, onBranch.Msg.GetPullRequests()[0].GetReviewDecision())
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED, onBranch.Msg.GetPullRequests()[2].GetState())
	assert.Equal(t, approved.ID, onBranch.Msg.GetPullRequests()[0].GetId())

	var walked []int32
	var cursor *string
	for {
		page, err := service.ListPullRequests(ctx, connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
			RepositoryUrl: weaveRepository,
			Pagination:    &paginationv1.Pagination{Limit: xptr.Ptr(int32(2)), Cursor: cursor},
		}))
		require.NoError(t, err)
		walked = append(walked, numbers(page.Msg.GetPullRequests())...)
		if page.Msg.NextPageToken == nil {
			break
		}
		cursor = page.Msg.NextPageToken
	}
	assert.Equal(t, []int32{4, 2, 1, 3}, walked, "the whole repository, two to a page, in one order")
}

// TestListPullRequestsRefusals covers a repository that names no forge and a cursor nobody
// could have been handed.
func TestListPullRequestsRefusals(t *testing.T) {
	t.Parallel()

	st := storetest.Open(t)
	service := NewHandler(NewHandlerParams{PullRequests: st.PullRequests})

	_, failure := service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{RepositoryUrl: "/local/path"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, git.ErrNotForgeRemote)

	_, failure = service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{
		RepositoryUrl: weaveRepository,
		Pagination:    &paginationv1.Pagination{Cursor: xptr.Ptr("not-a-token")},
	})
	require.NotNil(t, failure)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(failure))
}

const weaveRepository = "https://github.com/harrisoncramer/weave"

// storedPullRequest is a pull request from agent/x of the weave repository, merged when a
// stamp is given.
func storedPullRequest(providerID string, number int, opened time.Time, mergedAt *time.Time) pullrequeststore.UpsertParams {
	state := "open"
	if mergedAt != nil {
		state = "closed"
	}

	return pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             forge.KindGitHub,
		ProviderID:        providerID,
		URL:               "https://github.com/harrisoncramer/weave/pull/" + providerID,
		RepositoryURL:     weaveRepository,
		HeadRepositoryURL: weaveRepository,
		Number:            number,
		Title:             providerID,
		HeadBranch:        "agent/x",
		HeadSHA:           "abc",
		BaseBranch:        "main",
		BaseSHA:           "def",
		State:             state,
		OpenedAt:          opened,
		ChangedAt:         opened,
		MergedAt:          mergedAt,
		ClosedAt:          mergedAt,
	}
}
