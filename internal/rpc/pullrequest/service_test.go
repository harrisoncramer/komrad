package pullrequest

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"
	"weave/lib/xptr"
	paginationv1 "weave/proto/gen/pagination/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	"weave/proto/gen/pullrequest/v1/pullrequestv1weave"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeForge struct {
	pullRequests []forge.PullRequest
	hasMore      bool
	params       forge.ListParams
}

func (f *fakeForge) Kind() string                           { return forge.KindGitHub }
func (f *fakeForge) Verify(context.Context) (string, error) { return "", nil }
func (f *fakeForge) Get(context.Context, string) (forge.PullRequest, error) {
	return forge.PullRequest{}, nil
}

func (f *fakeForge) ListUpdated(context.Context, forge.ListUpdatedParams) (*forge.ListUpdatedResult, error) {
	return &forge.ListUpdatedResult{}, nil
}

func (f *fakeForge) GetReviewDecision(context.Context, git.RemoteURL, int) (string, error) {
	return "", nil
}

func (f *fakeForge) List(_ context.Context, params forge.ListParams) (*forge.ListResult, error) {
	f.params = params

	return &forge.ListResult{PullRequests: f.pullRequests, HasMore: f.hasMore}, nil
}

type fakeFinder struct {
	forge *fakeForge
}

func (f *fakeFinder) ConnectionFor(_ context.Context, host string) (*forgestore.Connection, error) {
	if host != "github.com" {
		return nil, store.ErrNotFound.Saying("%s is not connected", host)
	}

	return &forgestore.Connection{Host: host, Kind: forge.KindGitHub}, nil
}

func (f *fakeFinder) ForConnection(forgestore.Connection) (forge.Forge, error) { return f.forge, nil }

// TestListPullRequests covers the remote picking the connection by host, the branch reaching
// the forge, and each pull request mapped through with its repositories canonical.
func TestListPullRequests(t *testing.T) {
	t.Parallel()

	merged := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	upstream := git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/weave",
	}
	fake := &fakeForge{pullRequests: []forge.PullRequest{{
		Forge:      forge.KindGitHub,
		ProviderID: "PR_1",
		URL:        "https://github.com/harrisoncramer/weave/pull/1",
		Repository: upstream,
		Number:     1,
		Title:      "one",
		HeadBranch: "agent/x",
		MergedAt:   &merged,
		ClosedAt:   &merged,
	}}}
	service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewPullRequestService(&fakeFinder{forge: fake})}

	resp, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RemoteUrl: "git@github.com:harrisoncramer/weave.git",
		Branch:    "agent/x",
	}))
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer/weave", fake.params.Repo.Path)
	assert.Equal(t, "agent/x", fake.params.Branch)
	assert.Equal(t, 1, fake.params.Page)
	assert.Equal(t, 20, fake.params.PerPage, "twenty to a page when the caller says nothing")
	assert.Nil(t, resp.Msg.NextPageToken, "the only page carries no token")
	require.Len(t, resp.Msg.GetPullRequests(), 1)

	pullRequest := resp.Msg.GetPullRequests()[0]
	assert.Equal(t, "https://github.com/harrisoncramer/weave", pullRequest.GetRepositoryUrl())
	assert.Empty(t, pullRequest.GetHeadRepositoryUrl(), "a head the forge no longer has is empty rather than https:///")
	assert.NotNil(t, pullRequest.MergedAt)
}

// TestListPullRequestsPages covers the token round trip: a page with more behind it hands
// back a token, and that token asks the forge for the page after at the size the first was
// cut at, whatever limit travels beside it.
func TestListPullRequestsPages(t *testing.T) {
	t.Parallel()

	fake := &fakeForge{hasMore: true}
	service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewPullRequestService(&fakeFinder{forge: fake})}

	first, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr(int32(50))},
	}))
	require.NoError(t, err)
	require.NotNil(t, first.Msg.NextPageToken)
	assert.Equal(t, 50, fake.params.PerPage)

	fake.hasMore = false
	second, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
		Pagination: &paginationv1.Pagination{Cursor: first.Msg.NextPageToken},
	}))
	require.NoError(t, err)
	assert.Equal(t, 2, fake.params.Page)
	assert.Equal(t, 50, fake.params.PerPage, "the token carries the size, so a dropped limit reads the same window")
	assert.Nil(t, second.Msg.NextPageToken)
}

// TestListPullRequestsRefusesABadToken covers a cursor that is not one and a cursor nobody
// could have been handed.
func TestListPullRequestsRefusesABadToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cursor string
	}{
		{
			name:   "not a token",
			cursor: "not-a-token",
		},
		{
			name:   "a page before the first",
			cursor: base64.URLEncoding.EncodeToString([]byte(`{"page":0,"per_page":20}`)),
		},
		{
			name:   "a token with no page size",
			cursor: base64.URLEncoding.EncodeToString([]byte(`{"page":2}`)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := pullrequestv1weave.PullRequestServiceAdapter{Impl: NewPullRequestService(&fakeFinder{forge: &fakeForge{}})}

			_, err := service.ListPullRequests(t.Context(), connect.NewRequest(&pullrequestv1.ListPullRequestsRequest{
				RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
				Pagination: &paginationv1.Pagination{Cursor: xptr.Ptr(tt.cursor)},
			}))
			require.Error(t, err)
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

// TestListPullRequestsRefusals covers a remote that is not one, and a host nothing is
// connected to.
func TestListPullRequestsRefusals(t *testing.T) {
	t.Parallel()

	service := NewPullRequestService(&fakeFinder{forge: &fakeForge{}})

	_, failure := service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{RemoteUrl: "/local/path", Branch: "main"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, git.ErrNotForgeRemote)

	_, failure = service.ListPullRequests(t.Context(), &pullrequestv1.ListPullRequestsRequest{RemoteUrl: "git@gitlab.com:o/r.git", Branch: "main"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, store.ErrNotFound)
}
