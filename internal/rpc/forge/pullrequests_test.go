package forge

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/store"
	"weave/lib/xptr"
	forgev1 "weave/proto/gen/forge/v1"
	forgev1weave "weave/proto/gen/forge/v1/forgev1weave"
	paginationv1 "weave/proto/gen/pagination/v1"

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

func (f *fakeForge) ListUpdated(context.Context, forge.ListUpdatedParams) (*forge.Page, error) {
	return &forge.Page{}, nil
}

func (f *fakeForge) ListChanged(context.Context, forge.ListChangedParams) (*forge.Page, error) {
	return &forge.Page{}, nil
}

func (f *fakeForge) Standings(context.Context, git.RemoteURL, []int) (map[int]forge.Standing, error) {
	return map[int]forge.Standing{}, nil
}

func (f *fakeForge) List(_ context.Context, params forge.ListParams) (*forge.Page, error) {
	f.params = params

	return &forge.Page{
		PullRequests: f.pullRequests,
		HasMore:      f.hasMore,
		Cursor:       "end-of-page",
	}, nil
}

// TestListForgePullRequests covers the remote picking the connection by host, the branch reaching
// the forge, and each pull request mapped through with its repositories canonical.
func TestListForgePullRequests(t *testing.T) {
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
	service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(&fakeForges{connection: connected(), forge: fake})}

	resp, err := service.ListForgePullRequests(t.Context(), connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
		RemoteUrl: "git@github.com:harrisoncramer/weave.git",
		Branch:    "agent/x",
	}))
	require.NoError(t, err)
	assert.Equal(t, "harrisoncramer/weave", fake.params.Repo.Path)
	assert.Equal(t, "agent/x", fake.params.Branch)
	assert.Empty(t, fake.params.Cursor)
	assert.Equal(t, 20, fake.params.PerPage, "twenty to a page when the caller says nothing")
	assert.Nil(t, resp.Msg.NextPageToken, "the only page carries no token")
	require.Len(t, resp.Msg.GetPullRequests(), 1)

	pullRequest := resp.Msg.GetPullRequests()[0]
	assert.Equal(t, "https://github.com/harrisoncramer/weave", pullRequest.GetRepositoryUrl())
	assert.Empty(t, pullRequest.GetHeadRepositoryUrl(), "a head the forge no longer has is empty rather than https:///")
	assert.NotNil(t, pullRequest.MergedAt)
}

// TestListForgePullRequestsPages covers the token round trip: a page with more behind it hands
// back a token carrying the forge's cursor, and that token asks the forge for the page after
// at the size the first was cut at, whatever limit travels beside it.
func TestListForgePullRequestsPages(t *testing.T) {
	t.Parallel()

	fake := &fakeForge{hasMore: true}
	service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(&fakeForges{connection: connected(), forge: fake})}

	first, err := service.ListForgePullRequests(t.Context(), connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr(int32(50))},
	}))
	require.NoError(t, err)
	require.NotNil(t, first.Msg.NextPageToken)
	assert.Equal(t, 50, fake.params.PerPage)

	fake.hasMore = false
	second, err := service.ListForgePullRequests(t.Context(), connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
		RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
		Pagination: &paginationv1.Pagination{Cursor: first.Msg.NextPageToken},
	}))
	require.NoError(t, err)
	assert.Equal(t, "end-of-page", fake.params.Cursor)
	assert.Equal(t, 50, fake.params.PerPage, "the token carries the size, so a dropped limit reads the same window")
	assert.Nil(t, second.Msg.NextPageToken)
}

// TestListForgePullRequestsRefusesABadToken covers a cursor that is not one and a cursor nobody
// could have been handed.
func TestListForgePullRequestsRefusesABadToken(t *testing.T) {
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
			name:   "a token with no cursor",
			cursor: base64.URLEncoding.EncodeToString([]byte(`{"per_page":20}`)),
		},
		{
			name:   "a token with no page size",
			cursor: base64.URLEncoding.EncodeToString([]byte(`{"cursor":"Y3Vyc29yOjM="}`)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := forgev1weave.ForgeServiceAdapter{Impl: NewHandler(&fakeForges{connection: connected(), forge: &fakeForge{}})}

			_, err := service.ListForgePullRequests(t.Context(), connect.NewRequest(&forgev1.ListForgePullRequestsRequest{
				RemoteUrl:  "git@github.com:harrisoncramer/weave.git",
				Pagination: &paginationv1.Pagination{Cursor: xptr.Ptr(tt.cursor)},
			}))
			require.Error(t, err)
			assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}

// TestListForgePullRequestsRefusals covers a remote that is not one, and a host nothing is
// connected to.
func TestListForgePullRequestsRefusals(t *testing.T) {
	t.Parallel()

	service := NewHandler(&fakeForges{connection: connected(), forge: &fakeForge{}})

	_, failure := service.ListForgePullRequests(t.Context(), &forgev1.ListForgePullRequestsRequest{RemoteUrl: "/local/path", Branch: "main"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, git.ErrNotForgeRemote)

	_, failure = service.ListForgePullRequests(t.Context(), &forgev1.ListForgePullRequestsRequest{RemoteUrl: "git@gitlab.com:o/r.git", Branch: "main"})
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, store.ErrNotFound)
}
